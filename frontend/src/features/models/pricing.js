import { btn, cell, emptyRow, on, row } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, toast } from '../../shared/dom';
import { api } from '../../api/client';
import { listen } from '../../shared/lifecycle';
const pricingState = {};
pricingState.priceCache = {};

pricingState.priceEditing = null;

pricingState.priceSaving = false;

export function renderPricing() {
    var ids = Object.keys(pricingState.priceCache);
    ((serverState.data || {}).models || []).forEach(function (m) { if (m.source !== "alias" && ids.indexOf(m.id) < 0)
        ids.push(m.id); });
    var tb = domState.$("#pricingRows");
    tb.textContent = "";
    ids.sort().forEach(function (id) {
        var p = pricingState.priceCache[id], ops = cell("");
        ops.appendChild(btn("编辑", "xs", function () { openPriceEditor(id); }));
        tb.appendChild(row([cell(id, "mono"), cell("USD", "mono"), cell(p ? String(p.input) : "未配置", "mono"), cell(p ? String(p.output) : "未配置", "mono"), cell(p ? String(p.cached) : "未配置", "mono"), cell(p ? p.source + " · " + p.updated_at : "未核实官方单价，可自定义", "muted pricing-source"), ops]));
    });
    if (!ids.length)
        tb.appendChild(emptyRow(7, "暂无模型，请先刷新模型目录"));
}

export function loadPricing() {
    if (!serverState.data)
        return Promise.resolve();
    return api("/admin/api/pricing").then(function (d) { pricingState.priceCache = d.prices || {}; renderPricing(); }).catch(function (e) { if (isAbortError(e)) return; toast("价格表加载失败：" + e.message, "err"); });
}

export function openPriceEditor(id) {
    if (pricingState.priceSaving)
        return;
    pricingState.priceEditing = id;
    var p = pricingState.priceCache[id];
    domState.$("#priceEditModel").textContent = id;
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
    var price = { currency: "USD", source: "管理员自定义参考价" }, fields = { input: "#priceEditInput", output: "#priceEditOutput", cached: "#priceEditCached" };
    for (var field in fields) {
        var input = domState.$(fields[field]);
        if (input.value.trim() === "" || !input.checkValidity()) {
            domState.$("#priceEditError").textContent = "请填写三项有效单价，0 表示免费";
            input.focus();
            return;
        }
        price[field] = Number(input.value);
    }
    pricingState.priceSaving = true;
    domState.$("#priceEditError").textContent = "";
    ["#priceEditSave", "#priceEditCancel", "#priceEditClose", "#priceEditInput", "#priceEditOutput", "#priceEditCached"].forEach(function (id) { domState.$(id).disabled = true; });
    return api("/admin/api/pricing", { method: "POST", body: { model: pricingState.priceEditing, price: price } }).then(function (result) {
        pricingState.priceCache = result.prices;
        renderPricing();
        domState.$("#priceEditDialog").close();
        pricingState.priceEditing = null;
        toast("价格已保存，仅影响新请求", "ok");
    }).catch(function (e) { if (isAbortError(e)) return; domState.$("#priceEditError").textContent = "保存失败：" + e.message; }).finally(function () { pricingState.priceSaving = false; ["#priceEditSave", "#priceEditCancel", "#priceEditClose", "#priceEditInput", "#priceEditOutput", "#priceEditCached"].forEach(function (id) { domState.$(id).disabled = false; }); });
}

export function mountPricing() {
on("#reloadPricing", loadPricing);
on("#priceEditSave", savePriceEditor);
on("#priceEditCancel", closePriceEditor);
on("#priceEditClose", closePriceEditor);
listen(domState.$("#priceEditDialog"), "cancel", function (e) { if (pricingState.priceSaving)
    e.preventDefault();
else
    pricingState.priceEditing = null; });
}
