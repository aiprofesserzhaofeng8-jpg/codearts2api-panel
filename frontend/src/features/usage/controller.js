import { t as tr, dateLocale } from '../../i18n';
import { refresh } from '../../shared/refresh';
import { btn, cell, emptyRow, mk, on, pill, row } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { isAbortError } from '../../api/client';
// Native DOM controller: this feature exclusively owns its static Vue subtree.
import { domState, fmtTime, toast } from '../../shared/dom';
import { listen } from '../../shared/lifecycle';
import { api } from '../../api/client';
const usageState = {};
export function usageNumber(n) {
    n = Number(n || 0);
    if (n >= 1000000)
        return (n / 1000000).toFixed(2).replace(/\.00$/, "") + "M";
    if (n >= 1000)
        return (n / 1000).toFixed(2).replace(/\.00$/, "") + "K";
    return n.toLocaleString(dateLocale);
}

export function usageIcon(kind) {
    var paths = {
        requests: ["M7 3h7l4 4v14H6V3h1Z", "M14 3v5h4M9 12h6M9 16h6"],
        tokens: ["m12 3 9 5-9 5-9-5 9-5Z", "M3 8v9l9 5 9-5V8M12 13v9"],
        cost: ["M15 8h-4a2 2 0 0 0 0 4h2a2 2 0 0 1 0 4H9M12 6v12"],
        latency: ["M12 6v6h4"]
    };
    var icon = mk("span", "usage-icon " + kind);
    var ns = document.querySelector("nav svg").namespaceURI, svg = document.createElementNS(ns, "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    if (kind === "cost" || kind === "latency") {
        var circle = document.createElementNS(ns, "circle");
        circle.setAttribute("cx", "12");
        circle.setAttribute("cy", "12");
        circle.setAttribute("r", "9");
        svg.appendChild(circle);
    }
    paths[kind].forEach(function (d) { var path = document.createElementNS(ns, "path"); path.setAttribute("d", d); svg.appendChild(path); });
    icon.appendChild(svg);
    return icon;
}

export function money(amount, currency) { return currency === "USD" ? "$" + Number(amount || 0).toFixed(6) : "—"; }

export function renderUsage(s) {
    var known = Number(s.usage_requests || 0), input = Number(s.input_tokens || 0), output = Number(s.output_tokens || 0);
    var cached = Math.min(input, Number(s.cached_tokens || 0)), total = input + output;
    var samples = Number(s.latency_samples || 0), requests = Number(s.total_requests || 0), failed = Number(s.failed_requests || 0);
    var measured = function (n) { return known ? usageNumber(n) : "—"; };
    var amounts = [];
    if (s.priced_usd_requests)
        amounts.push("$" + Number(s.estimated_usd).toFixed(4));
    var cards = [
        { kind: "requests", label: tr("总请求数"), value: requests.toLocaleString(dateLocale), sub: tr("所选时间范围内") },
        { kind: "tokens", label: tr("总 Token"), value: measured(total), sub: tr("输入: ") + measured(input - cached) + tr(" / 输出: ") + measured(output) + tr(" / 缓存: ") + measured(cached) },
        { kind: "cost", label: tr("估算费用"), value: amounts.join(" + ") || "—", sub: tr("已计价 ") + (s.priced_requests || 0) + " / " + requests + tr(" 次") },
        { kind: "latency", label: tr("平均耗时"), value: samples ? (s.total_latency_ms / samples / 1000).toFixed(2) + "s" : "—", sub: "" }
    ];
    var box = domState.$("#kpis");
    box.textContent = "";
    cards.forEach(function (item) {
        var card = mk("div", "usage-kpi " + item.kind);
        card.appendChild(usageIcon(item.kind));
        var content = mk("div", "usage-kpi-content");
        content.appendChild(mk("div", "usage-label", item.label));
        content.appendChild(mk("div", "usage-value", item.value));
        if (item.sub)
            content.appendChild(mk("div", "usage-sub", item.sub));
        card.appendChild(content);
        box.appendChild(card);
    });
    domState.$("#usageCoverage").textContent = tr("用量已报告 ") + known.toLocaleString(dateLocale) + " / " + requests.toLocaleString(dateLocale) + tr(" 次") + (s.partial_usage_requests ? " · " + s.partial_usage_requests + tr(" 次部分报告") : "");
    domState.$("#usageCoverage").title = tr("缓存包含在输入 Token 中，推理包含在输出 Token 中。未报告用量显示为 —；费用按价格表估算，部分报告仅计已知用量。");
    var breakdown = domState.$("#usageBreakdown");
    breakdown.textContent = "";
    [{ label: tr("输入（未缓存）"), value: input - cached, tone: "input" }, { label: tr("输出"), value: output, tone: "output" }, { label: tr("缓存命中"), value: cached, tone: "cached" }].forEach(function (item) {
        var line = mk("div", "usage-token-row " + item.tone), title = mk("div", "usage-token-label");
        title.appendChild(mk("span", "", item.label));
        title.appendChild(mk("strong", "", measured(item.value)));
        line.appendChild(title);
        var meter = document.createElement("progress");
        meter.max = total || 1;
        meter.value = item.value;
        meter.setAttribute("aria-label", item.label);
        line.appendChild(meter);
        breakdown.appendChild(line);
    });
    var health = domState.$("#usageHealth");
    health.textContent = "";
    [{ label: tr("成功请求"), value: usageNumber(Math.max(0, requests - failed)), tone: "good" }, { label: tr("失败请求"), value: usageNumber(failed), tone: "bad" },
        { label: tr("成功率"), value: requests ? ((requests - failed) / requests * 100).toFixed(1) + "%" : "—", tone: "" }, { label: tr("摘要 / 失败 / 重试"), value: (s.summary_requests || 0) + " / " + (s.summary_failures || 0) + " / " + (s.summary_retries || 0), tone: "" }, { label: tr("最终截断"), value: usageNumber(s.truncated_requests || 0), tone: "" }].forEach(function (item) {
        var line = mk("div", "usage-result " + item.tone);
        line.appendChild(mk("span", "", item.label));
        line.appendChild(mk("strong", "", item.value));
        health.appendChild(line);
    });
}

usageState.usageAnalysis = null;

usageState.usageMetrics = { models: "tokens", providers: "tokens", protocols: "requests" };

export function usageSVG(tag, attrs, text) {
    var node = document.createElementNS(document.querySelector("nav svg").namespaceURI, tag);
    Object.keys(attrs || {}).forEach(function (key) { node.setAttribute(key, String(attrs[key])); });
    if (text !== undefined)
        node.textContent = text;
    return node;
}

export function usageMetricValue(stats, metric) {
    if (metric === "requests")
        return Number(stats.total_requests || 0);
    if (metric === "cost")
        return Number(stats.estimated_usd || 0);
    return Number(stats.input_tokens || 0) + Number(stats.output_tokens || 0);
}

export function usageMetricText(value, metric) { return metric === "cost" ? "$" + Number(value).toFixed(4) : usageNumber(value); }

export function usageSeriesName(name, kind) {
    var providers = { codearts: tr("华为云码道"), workbuddy: "WorkBuddy", trae: "Trae" };
    var protocols = { chat_completions: "Chat Completions", responses: "Responses", messages: "Messages", anthropic_messages: "Messages", search: tr("联网搜索"), growth: tr("成长任务"), unknown: tr("未记录接口") };
    return kind === "providers" ? (providers[name] || name) : kind === "protocols" ? (protocols[name] || name) : name;
}

export function renderUsageDistribution(kind, holder) {
    var metric = usageState.usageMetrics[kind], labels = { tokens: "Token", requests: tr("请求"), cost: tr("估算费用") };
    var tabs = document.querySelector('[data-usage-metric="' + kind + '"]');
    tabs.textContent = "";
    Object.keys(labels).forEach(function (key) { var button = mk("button", "", labels[key]); button.type = "button"; button.setAttribute("aria-pressed", String(key === metric)); button.onclick = function () { usageState.usageMetrics[kind] = key; renderUsageDistribution(kind, holder); }; tabs.appendChild(button); });
    holder.textContent = "";
    var source = (usageState.usageAnalysis || {})[kind] || [];
    var values = source.map(function (item) { return { name: usageSeriesName(item.name, kind), stats: item.stats, value: usageMetricValue(item.stats, metric) }; }).sort(function (a, b) { return b.value - a.value; });
    if (!values.length) {
        holder.appendChild(mk("div", "usage-chart-empty", tr("暂无统计数据")));
        return;
    }
    var total = values.reduce(function (sum, item) { return sum + item.value; }, 0), ring = mk("div", "usage-donut"), svg = usageSVG("svg", { viewBox: "0 0 220 220", role: "img", "aria-label": labels[metric] + tr("分布") });
    svg.appendChild(usageSVG("circle", { cx: 110, cy: 110, r: 78, class: "usage-donut-track" }));
    var center = mk("div", "usage-donut-center"), value = mk("strong", "", usageMetricText(total, metric)), caption = mk("span", "", labels[metric] + tr("合计"));
    center.appendChild(value);
    center.appendChild(caption);
    var legend = mk("div", "usage-chart-legend"), table = mk("table", ""), head = mk("thead", ""), headrow = mk("tr", ""), body = mk("tbody", ""), offset = 0, circumference = 2 * Math.PI * 78;
    [kind === "models" ? tr("模型") : kind === "providers" ? tr("上游") : tr("接口"), tr("请求"), "Token", tr("估算费用")].forEach(function (label) { headrow.appendChild(mk("th", "", label)); });
    head.appendChild(headrow);
    table.appendChild(head);
    table.appendChild(body);
    legend.appendChild(table);
    function reset() { value.textContent = usageMetricText(total, metric); caption.textContent = labels[metric] + tr("合计"); ring.classList.remove("active"); }
    values.forEach(function (item, index) {
        var percent = total ? item.value / total * 100 : 0, palette = "usage-palette-" + (index % 8);
        function highlight() { value.textContent = total ? percent.toFixed(1) + "%" : "—"; caption.textContent = item.name; ring.classList.add("active"); }
        if (item.value > 0) {
            var circle = usageSVG("circle", { cx: 110, cy: 110, r: 78, class: "usage-donut-segment " + palette, "stroke-dasharray": circumference * item.value / total + " " + circumference, "stroke-dashoffset": -offset, tabindex: 0 });
            circle.appendChild(usageSVG("title", {}, item.name + "：" + usageMetricText(item.value, metric) + "（" + percent.toFixed(1) + "%）"));
            circle.addEventListener("mouseenter", highlight);
            circle.addEventListener("mouseleave", reset);
            circle.addEventListener("focus", highlight);
            circle.addEventListener("blur", reset);
            svg.appendChild(circle);
            offset += circumference * item.value / total;
        }
        var entry = mk("tr", "usage-chart-legend-item");
        entry.tabIndex = 0;
        entry.title = item.name;
        entry.setAttribute("aria-label", item.name + "，" + usageNumber(item.stats.total_requests) + tr(" 次请求"));
        var name = cell(""), dot = mk("span", "usage-legend-dot");
        dot.classList.add(palette);
        name.appendChild(dot);
        name.appendChild(document.createTextNode(item.name));
        [name, cell(usageNumber(item.stats.total_requests)), cell(item.stats.usage_requests ? usageNumber(usageMetricValue(item.stats, "tokens")) : "—"), cell(item.stats.priced_usd_requests ? usageMetricText(item.stats.estimated_usd, "cost") : "—")].forEach(function (td) { entry.appendChild(td); });
        entry.addEventListener("mouseenter", highlight);
        entry.addEventListener("mouseleave", reset);
        entry.addEventListener("focus", highlight);
        entry.addEventListener("blur", reset);
        body.appendChild(entry);
    });
    ring.appendChild(svg);
    ring.appendChild(center);
    holder.appendChild(ring);
    holder.appendChild(legend);
}

export function renderUsageTrend() {
    var holder = domState.$("#usageTrendChart");
    holder.textContent = "";
    var points = (usageState.usageAnalysis || {}).trend || [], days = (usageState.usageAnalysis || {}).bucket_days || 1;
    domState.$("#usageTrendPeriod").textContent = days > 1 ? tr("每 ") + days + tr(" 天汇总") : tr("按日统计");
    if (!points.some(function (p) { return p.stats.usage_requests > 0; })) {
        holder.appendChild(mk("div", "usage-chart-empty", tr("暂无已报告的 Token 用量")));
        return;
    }
    var series = [{ label: tr("输入"), key: "input", palette: 0 }, { label: tr("输出"), key: "output", palette: 1 }, { label: tr("缓存读取"), key: "cached", palette: 5 }];
    function amounts(p) { var s = p.stats, input = Number(s.input_tokens || 0), cached = Math.min(input, Number(s.cached_tokens || 0)); return { input: input - cached, output: Number(s.output_tokens || 0), cached: cached }; }
    var values = points.map(amounts), max = 1;
    values.forEach(function (v) { max = Math.max(max, v.input, v.output, v.cached); });
    max = Math.ceil(max / 4) * 4;
    var legend = mk("div", "usage-trend-legend");
    series.forEach(function (s) { var item = mk("span", ""), dot = mk("i", "usage-legend-dot"); dot.classList.add("usage-palette-" + s.palette); item.appendChild(dot); item.appendChild(document.createTextNode(s.label)); legend.appendChild(item); });
    holder.appendChild(legend);
    var svg = usageSVG("svg", { viewBox: "0 0 520 170", role: "img", "aria-label": tr("所选范围的输入、输出与缓存 Token 趋势"), preserveAspectRatio: "none" });
    var x = function (i) { return points.length === 1 ? 284 : 48 + i * 452 / (points.length - 1); }, y = function (v) { return 135 - v / max * 120; };
    for (var tick = 0; tick <= 4; tick++) {
        var yy = 15 + tick * 30;
        svg.appendChild(usageSVG("line", { x1: 48, y1: yy, x2: 500, y2: yy, class: "usage-trend-grid" }));
        svg.appendChild(usageSVG("text", { x: 40, y: yy + 3, "text-anchor": "end", class: "usage-trend-label" }, usageNumber(max * (4 - tick) / 4)));
    }
    series.forEach(function (s) {
        var segments = [], current = [];
        points.forEach(function (p, i) { if (p.stats.total_requests > 0 && !p.stats.usage_requests) {
            if (current.length)
                segments.push(current);
            current = [];
            return;
        } current.push([x(i), y(values[i][s.key])]); });
        if (current.length)
            segments.push(current);
        segments.forEach(function (segment) { var path = segment.map(function (p, i) { if (!i)
            return "M" + p[0] + " " + p[1]; var prev = segment[i - 1], dx = (p[0] - prev[0]) * .3; return "C" + (prev[0] + dx) + " " + prev[1] + " " + (p[0] - dx) + " " + p[1] + " " + p[0] + " " + p[1]; }).join(" "); if (segment.length > 1)
            svg.appendChild(usageSVG("path", { d: path + " L" + segment[segment.length - 1][0] + " 135 L" + segment[0][0] + " 135 Z", class: "usage-trend-area usage-palette-" + s.palette })); svg.appendChild(usageSVG("path", { d: path, class: "usage-trend-line usage-palette-" + s.palette })); if (segment.length === 1)
            svg.appendChild(usageSVG("circle", { cx: segment[0][0], cy: segment[0][1], r: 3, fill: "currentColor", class: "usage-palette-" + s.palette })); });
    });
    var caption = mk("div", "usage-trend-caption", tr("悬停或聚焦图表查看每日用量"));
    var indices = [0, Math.floor((points.length - 1) / 2), points.length - 1].filter(function (n, i, a) { return a.indexOf(n) === i; });
    points.forEach(function (p, i) {
        var date = new Date(p.time * 1000), label = (date.getMonth() + 1) + "/" + date.getDate();
        if (indices.includes(i))
            svg.appendChild(usageSVG("text", { x: x(i), y: 159, "text-anchor": "middle", class: "usage-trend-label" }, label));
        var width = points.length === 1 ? 452 : 452 / (points.length - 1), left = Math.max(48, x(i) - width / 2), right = Math.min(500, x(i) + width / 2);
        var hit = usageSVG("rect", { x: left, y: 12, width: right - left, height: 128, class: "usage-trend-hover", tabindex: 0 });
        var text = localDate(date) + (days > 1 ? tr("起 ") + days + tr(" 天") : "") + " · " + (p.stats.total_requests > 0 && !p.stats.usage_requests ? tr("用量未报告") : tr("输入 ") + usageNumber(values[i].input) + tr(" / 输出 ") + usageNumber(values[i].output) + tr(" / 缓存 ") + usageNumber(values[i].cached));
        hit.setAttribute("aria-label", text);
        hit.appendChild(usageSVG("title", {}, text));
        listen(hit, "mouseenter", function () { caption.textContent = text; });
        listen(hit, "focus", function () { caption.textContent = text; });
        svg.appendChild(hit);
    });
    holder.appendChild(svg);
    holder.appendChild(caption);
}

export function renderUsageCharts(analysis) { usageState.usageAnalysis = analysis || null; renderUsageDistribution("models", domState.$("#usageModelsChart")); renderUsageDistribution("providers", domState.$("#usageProvidersChart")); renderUsageDistribution("protocols", domState.$("#usageProtocolsChart")); renderUsageTrend(); }

usageState.usageView = { page: 1, pageSize: 20, start: 0, end: 0, label: tr("全部已保存记录"), preset: "all", pages: 1, total: 0, busy: false, seq: 0 };

export function localDate(d) { return d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0"); }

export function closeUsageDatePicker(restoreFocus) { domState.$("#usageDatePopover").classList.add("hidden"); domState.$("#usageDateToggle").setAttribute("aria-expanded", "false"); if (restoreFocus)
    domState.$("#usageDateToggle").focus(); }

export function syncUsageDatePicker() { document.querySelectorAll("[data-usage-preset]").forEach(function (button) { button.setAttribute("aria-pressed", String(button.dataset.usagePreset === domState.$("#usagePreset").value)); }); }

export function setUsagePreset() {
    var preset = domState.$("#usagePreset").value, start = new Date(), end = new Date();
    if (preset === "custom")
        return;
    if (preset === "all") {
        domState.$("#usageStart").value = "";
        domState.$("#usageEnd").value = "";
    }
    else {
        if (preset === "yesterday") {
            start.setDate(start.getDate() - 1);
            end.setDate(end.getDate() - 1);
        }
        if (preset === "7" || preset === "30")
            start.setDate(start.getDate() - Number(preset) + 1);
        if (preset === "month")
            start.setDate(1);
        if (preset === "lastMonth") {
            start = new Date(start.getFullYear(), start.getMonth() - 1, 1);
            end = new Date(end.getFullYear(), end.getMonth(), 0);
        }
        domState.$("#usageStart").value = localDate(start);
        domState.$("#usageEnd").value = localDate(end);
    }
    return applyUsageDates();
}

export function applyUsageDates() {
    var a = domState.$("#usageStart").value, b = domState.$("#usageEnd").value;
    if ((!a) !== (!b) || a > b) {
        toast(tr("请选择有效的开始和结束日期"), "err");
        return;
    }
    var start = a ? new Date(a + "T00:00:00") : null, end = b ? new Date(b + "T00:00:00") : null;
    if ((start && !Number.isFinite(start.getTime())) || (end && !Number.isFinite(end.getTime()))) {
        toast(tr("日期格式无效"), "err");
        return;
    }
    if (end)
        end.setDate(end.getDate() + 1);
    usageState.usageView.start = start ? Math.floor(start.getTime() / 1000) : 0;
    usageState.usageView.end = end ? Math.floor(end.getTime() / 1000) : 0;
    usageState.usageView.label = a ? a + tr(" 至 ") + b : tr("全部已保存记录");
    usageState.usageView.page = 1;
    usageState.usageView.preset = domState.$("#usagePreset").value;
    syncUsageDatePicker();
    closeUsageDatePicker(domState.$("#usageDatePopover").contains(document.activeElement));
    return refreshUsageLogs();
}

export function renderUsagePagination() {
    var v = usageState.usageView, first = v.total ? (v.page - 1) * v.pageSize + 1 : 0, last = Math.min(v.total, v.page * v.pageSize);
    domState.$("#usagePageInfo").textContent = tr("显示 ") + first + "–" + last + tr(" 条，共 ") + v.total + tr(" 条");
    domState.$("#usagePageSize").value = String(v.pageSize);
    domState.$("#usagePageJump").max = String(v.pages);
    domState.$("#usagePageJump").value = String(v.page);
    var box = domState.$("#usagePageButtons");
    box.textContent = "";
    function pageButton(label, page, current) {
        var button = btn(label, "xs" + (current ? " primary" : ""), function () { usageState.usageView.page = page; return refreshUsageLogs(); });
        button.disabled = v.busy || page < 1 || page > v.pages || current;
        if (current)
            button.setAttribute("aria-current", "page");
        button.setAttribute("aria-label", label === tr("上一页") || label === tr("下一页") ? label : tr("第 ") + page + tr(" 页"));
        box.appendChild(button);
    }
    pageButton(tr("上一页"), v.page - 1, false);
    var pages = [1];
    for (var n = Math.max(2, v.page - 2); n <= Math.min(v.pages - 1, v.page + 2); n++)
        pages.push(n);
    if (v.pages > 1)
        pages.push(v.pages);
    pages.forEach(function (n, i) { if (i && n - pages[i - 1] > 1)
        box.appendChild(mk("span", "pagination-ellipsis", "…")); pageButton(String(n), n, n === v.page); });
    pageButton(tr("下一页"), v.page + 1, false);
    domState.$("#usageJump").disabled = v.busy;
    domState.$("#usagePageSize").disabled = v.busy;
}

export function jumpUsagePage() {
    var n = Number(domState.$("#usagePageJump").value);
    if (!Number.isInteger(n) || n < 1 || n > usageState.usageView.pages) {
        toast(tr("请输入 1 至 ") + usageState.usageView.pages + tr(" 的页码"), "err");
        return;
    }
    usageState.usageView.page = n;
    return refreshUsageLogs();
}

export function refreshUsageLogs() {
    var seq = ++usageState.usageView.seq;
    usageState.usageView.busy = true;
    renderUsagePagination();
    domState.$("#usageLogs").setAttribute("aria-busy", "true");
    var query = new URLSearchParams({ page: String(usageState.usageView.page), page_size: String(usageState.usageView.pageSize) });
    query.set("time_zone", Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
    [["provider", "#usageProvider"], ["model", "#usageModel"], ["protocol", "#usageProtocol"], ["status", "#usageStatus"]].forEach(function (pair) { if (domState.$(pair[1]).value)
        query.set(pair[0], domState.$(pair[1]).value); });
    if (usageState.usageView.start)
        query.set("start", String(usageState.usageView.start));
    if (usageState.usageView.end)
        query.set("end", String(usageState.usageView.end));
    return api("/admin/api/usage?" + query.toString()).then(function (d) {
        if (seq !== usageState.usageView.seq)
            return;
        usageState.usageView.page = d.page;
        usageState.usageView.pages = d.pages;
        usageState.usageView.total = d.total;
        renderUsage(d.stats || {});
        renderUsageCharts(d.analysis);
        var modelFilter = domState.$("#usageModel"), selectedModel = modelFilter.value;
        var modelIDs = new Set(Array.from(modelFilter.options).map(function (o) { return o.value; }).filter(Boolean));
        ((d.analysis || {}).models || []).forEach(function (item) { modelIDs.add(item.name); });
        ((serverState.data || {}).models || []).forEach(function (item) { modelIDs.add(item.id); });
        modelFilter.textContent = "";
        var allModels = mk("option", "", tr("全部模型"));
        allModels.value = "";
        modelFilter.appendChild(allModels);
        Array.from(modelIDs).sort().forEach(function (id) { var option = mk("option", "", id); option.value = id; modelFilter.appendChild(option); });
        modelFilter.value = selectedModel;
        domState.$("#usagePeriod").textContent = usageState.usageView.label;
        domState.$("#usageHistoryHint").textContent = tr("共 ") + d.total + tr(" 条请求记录");
        domState.$("#usageHistoryHint").title = tr("已保存 ") + d.retained + tr(" 条，最多保留 ") + d.retention_limit + tr(" 条。") + (d.oldest ? tr("最早 ") + fmtTime(d.oldest) : "");
        var tb = domState.$("#usageLogs");
        tb.textContent = "";
        var logs = d.logs || [];
        if (!logs.length) {
            tb.appendChild(emptyRow(7, tr("所选条件下暂无请求记录")));
            return;
        }
        logs.forEach(function (l) {
            var u = l.usage || {}, known = u.known, input = Number(u.input_tokens || 0), output = Number(u.output_tokens || 0), cached = Math.min(input, Number(u.cached_tokens || 0));
            var number = function (n) { return known ? (l.usage_incomplete ? "≥" : "") + n.toLocaleString(dateLocale) : "—"; };
            var status = cell("");
            status.appendChild(pill(l.status, l.status >= 400 || l.err ? "err" : "ok"));
            if (l.finish_reason)
                status.appendChild(mk("span", "usage-finish", l.finish_reason));
            if (l.request_id)
                status.title = tr("请求 ID: ") + l.request_id;
            if (l.err)
                status.title = (status.title || "") + " · " + l.err;
            var stamp = fmtTime(l.time).split(" "), date = cell("", "usage-log-date");
            date.appendChild(mk("span", "", stamp[0]));
            date.appendChild(mk("span", "usage-log-sub", stamp[1] || ""));
            var model = cell(""), name = mk("span", "usage-log-model", l.model);
            name.title = (l.requested_model && l.requested_model !== l.model ? l.requested_model + " → " : "") + l.model;
            model.appendChild(name);
            model.appendChild(mk("span", "usage-log-sub", usageSeriesName(l.provider || "codearts", "providers")));
            var tokens = cell(""), counts = mk("div", "usage-token-values");
            [{ n: input - cached, label: tr("输入（未缓存）"), tone: "usage-input", path: "M7 2v10m-4-4 4 4 4-4" }, { n: output, label: tr("输出"), tone: "usage-output", path: "M7 12V2m-4 4 4-4 4 4" }].forEach(function (item) { var span = mk("span", ""), icon = usageSVG("svg", { viewBox: "0 0 14 14", class: item.tone, "aria-hidden": "true" }); icon.appendChild(usageSVG("path", { d: item.path })); span.title = item.label; span.appendChild(icon); span.appendChild(document.createTextNode(number(item.n))); counts.appendChild(span); });
            tokens.appendChild(counts);
            tokens.appendChild(mk("span", "usage-cache-value", tr("缓存 ") + number(cached)));
            tokens.title = tr("总 Token：") + number(input + output) + (l.usage_incomplete ? tr("（用量部分报告）") : "");
            var protocol = cell(""), badge = mk("span", "usage-protocol-badge", usageSeriesName(l.protocol || "unknown", "protocols"));
            protocol.appendChild(badge);
            if (l.request_kind === "summary")
                protocol.appendChild(mk("span", "usage-log-sub", tr("摘要 ×") + (l.summary_attempts || 1)));
            tb.appendChild(row([model, protocol, status, tokens, cell(l.estimated_cost ? money(l.estimated_cost.amount, l.estimated_cost.currency) + (l.estimated_cost.partial ? tr("（部分）") : "") : "—", "usage-money"), cell((l.latency_ms / 1000).toFixed(2) + "s"), date]));
        });
    }).catch(function (e) { if (isAbortError(e)) return;
        if (seq !== usageState.usageView.seq)
            return;
        renderUsage({});
        renderUsageCharts(null);
        usageState.usageView.page = 1;
        usageState.usageView.pages = 1;
        usageState.usageView.total = 0;
        domState.$("#usagePeriod").textContent = usageState.usageView.label + tr(" · 加载失败，请重试");
        domState.$("#usageLogs").textContent = "";
        domState.$("#usageLogs").appendChild(emptyRow(7, tr("用量加载失败：") + e.message));
    }).finally(function () { if (seq === usageState.usageView.seq && domState.$("#usageLogs")) {
        usageState.usageView.busy = false;
        renderUsagePagination();
        domState.$("#usageLogs").setAttribute("aria-busy", "false");
    } });
}

export function mountUsage() {
["#usageProvider", "#usageModel", "#usageProtocol", "#usageStatus"].forEach(function (id) { listen(domState.$(id), "change", function () { usageState.usageView.page = 1; refreshUsageLogs(); }); });
on("#reloadUsage", refreshUsageLogs);
on("#resetUsageFilters", function () { ["#usageProvider", "#usageModel", "#usageProtocol", "#usageStatus"].forEach(function (id) { domState.$(id).value = ""; }); domState.$("#usagePreset").value = "all"; return setUsagePreset(); });
on("#usageDateToggle", function () { var open = domState.$("#usageDateToggle").getAttribute("aria-expanded") !== "true"; domState.$("#usageDateToggle").setAttribute("aria-expanded", String(open)); domState.$("#usageDatePopover").classList.toggle("hidden", !open); if (open) {
    domState.$("#usageStart").value = usageState.usageView.start ? localDate(new Date(usageState.usageView.start * 1000)) : "";
    domState.$("#usageEnd").value = usageState.usageView.end ? localDate(new Date(usageState.usageView.end * 1000 - 1)) : "";
    domState.$("#usagePreset").value = usageState.usageView.preset;
    syncUsageDatePicker();
    domState.$("#usageDatePresets").querySelector("button").focus();
} });
document.querySelectorAll("[data-usage-preset]").forEach(function (button) { listen(button, "click", function () { domState.$("#usagePreset").value = button.dataset.usagePreset; setUsagePreset(); closeUsageDatePicker(true); }); });
listen(document, "click", function (event) { if (!domState.$("#usageDatePicker").contains(event.target))
    closeUsageDatePicker(false); });
listen(document, "keydown", function (event) { if (event.key === "Escape" && domState.$("#usageDateToggle").getAttribute("aria-expanded") === "true") {
    event.preventDefault();
    closeUsageDatePicker(true);
} });
on("#applyUsageDates", applyUsageDates);
on("#usageJump", jumpUsagePage);
listen(domState.$("#usagePageJump"), "keydown", function (e) { if (e.key === "Enter")
    jumpUsagePage(); });
listen(domState.$("#usagePreset"), "change", setUsagePreset);
["#usageStart", "#usageEnd"].forEach(function (id) { listen(domState.$(id), "change", function () { domState.$("#usagePreset").value = "custom"; syncUsageDatePicker(); }); });
listen(domState.$("#usagePageSize"), "change", function () { usageState.usageView.pageSize = Number(this.value); usageState.usageView.page = 1; try {
    localStorage.setItem("codearts-usage-page-size", this.value);
}
catch { } refreshUsageLogs(); });
try {
    var savedSize = Number(localStorage.getItem("codearts-usage-page-size"));
    if ([20, 50, 100].indexOf(savedSize) >= 0)
        usageState.usageView.pageSize = savedSize;
}
catch { }
domState.$("#usageTimezone").textContent = tr("日期按浏览器时区：") + Intl.DateTimeFormat().resolvedOptions().timeZone;
on("#clearStats", function () {
    if (!confirm(tr("确定重置统计？")))
        return;
    api("/admin/api/stats", { method: "DELETE" }).then(function () { toast(tr("已重置"), "ok"); return refresh(); })
        .catch(function (e) { if (isAbortError(e)) return; toast(tr("失败：") + e.message, "err"); });
});
}
