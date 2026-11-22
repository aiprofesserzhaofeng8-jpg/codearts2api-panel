import { t as tr } from '../../i18n';
import { btn, cell, emptyRow, on, row } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, toast } from '../../shared/dom';
import { api } from '../../api/client';
import { listen } from '../../shared/lifecycle';
const pricingState = {};
pricingState.priceCache = Object.create(null);

const DEFAULT = Symbol("default");
pricingState.priceEditing = null;

pricingState.priceSaving = false;

export function renderPricing() {
    domState.$("#defaultPriceSummary").textContent = pricingState.defaultPrice ? tr("缺价模型使用全站默认价") : tr("全站默认价未设置");
    var ids = Object.keys(pricingState.priceCache);
    ((serverState.data || {}).models || []).forEach(function (m) { if (m.source !== "alias" && ids.indexOf(m.id) < 0)
        ids.push(m.id); });
    var tb = domState.$("#pricingRows");
    tb.textContent = "";
    ids.sort().forEach(function (id) {
        var p = pricingState.priceCache[id] || pricingState.defaultPrice, ops = cell("");
        ops.appendChild(btn(tr("编辑"), "xs row-action row-edit", function () { openPriceEditor(id); }));
        tb.appendChild(row([cell(id, "mono"), cell("USD", "mono"), cell(p ? String(p.input) : tr("未配置"), "mono"), cell(p ? String(p.output) : tr("未配置"), "mono"), cell(p ? String(p.cached) : tr("未配置"), "mono"), cell(p ? [tr(p.source), p.updated_at].filter(Boolean).join(" · ") : tr("未核实官方单价，可自定义"), "muted pricing-source"), ops]));
    });
    if (!ids.length)
        tb.appendChild(emptyRow(7, tr("暂无模型，请先刷新模型目录")));
}

export function loadPricing() {
    if (!serverState.data)
        return Promise.resolve();
    return api("/admin/api/pricing").then(function (d) { applyPrices(d); }).catch(function (e) { if (isAbortError(e)) return; toast(tr("价格表加载失败：") + e.message, "err"); });
}

function applyPrices(data) {
    pricingState.priceCache = Object.assign(Object.create(null), data.prices);
    pricingState.defaultPrice = data.default_price ?? null;
    renderPricing();
}
export function openPriceEditor(id) {
    if (pricingState.priceSaving)
        return;
    pricingState.priceEditing = id;
    var p = id === DEFAULT ? pricingState.defaultPrice : pricingState.priceCache[id] ?? pricingState.defaultPrice;
    domState.$("#priceEditTitle").textContent = id === DEFAULT ? tr("全站默认价") : tr("编辑模型价格");
    domState.$("#priceEditModel").textContent = id === DEFAULT ? tr("仅用于未设置模型价且缺少参考价的调用") : id;
    domState.$("#priceEditClear").hidden = id !== DEFAULT || !p;
    domState.$("#priceEditError").textContent = "";
    domState.$("#priceEditInput").value = p ? p.input : "";
    domState.$("#priceEditOutput").value = p ? p.output : "";
    domState.$("#priceEditCached").value = p ? p.cached : "";
    domState.$("#priceEditDialog").showModal();
    domState.$("#priceEditInput").focus();
}

export function closePriceEditor() { if (!pricingState.priceSaving) {
    domState.$("#priceEditDialog").close();
    pricingState.priceEditing = null;
} }

export function savePriceEditor() {
    if (pricingState.priceSaving || !pricingState.priceEditing)
        return;
    var price = { currency: "USD", source: tr("管理员自定义参考价") }, fields = { input: "#priceEditInput", output: "#priceEditOutput", cached: "#priceEditCached" };
    for (var field in fields) {
        var input = domState.$(fields[field]);
        if (input.value.trim() === "" || !input.checkValidity()) {
            domState.$("#priceEditError").textContent = tr("请填写三项有效单价，0 表示免费");
            input.focus();
            return;
        }
        price[field] = Number(input.value);
    }
    const isDefault = pricingState.priceEditing === DEFAULT;
    return submitPrice(isDefault ? "/admin/api/pricing/default" : "/admin/api/pricing",
        isDefault ? {method:"PUT", body:price} : {method:"POST", body:{model:pricingState.priceEditing,price}});
}

function submitPrice(path, options) {
    if (pricingState.priceSaving) return;
    pricingState.priceSaving = true;
    domState.$("#priceEditError").textContent = "";
    ["#priceEditClear", "#priceEditSave", "#priceEditCancel", "#priceEditClose", "#priceEditInput", "#priceEditOutput", "#priceEditCached"].forEach(function (id) { domState.$(id).disabled = true; });
    return api(path, options).then(function (result) {
        applyPrices(result);
        domState.$("#priceEditDialog").close();
        pricingState.priceEditing = null;
        toast(tr("价格已保存，仅影响新请求"), "ok");
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#priceEditError").textContent = tr("保存失败：") + e.message; }).finally(function () { pricingState.priceSaving = false; ["#priceEditClear", "#priceEditSave", "#priceEditCancel", "#priceEditClose", "#priceEditInput", "#priceEditOutput", "#priceEditCached"].forEach(function (id) { domState.$(id).disabled = false; }); });
}

export function mountPricing() {
on("#reloadPricing", loadPricing);
on("#editDefaultPrice", () => openPriceEditor(DEFAULT));
on("#priceEditClear", () => submitPrice("/admin/api/pricing/default", {method:"DELETE"}));
on("#priceEditSave", savePriceEditor);
on("#priceEditCancel", closePriceEditor);
on("#priceEditClose", closePriceEditor);
listen(domState.$("#priceEditDialog"), "cancel", function (e) { if (pricingState.priceSaving)
    e.preventDefault();
else
    pricingState.priceEditing = null; });
}
