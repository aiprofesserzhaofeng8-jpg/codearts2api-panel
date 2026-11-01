import { listen } from './lifecycle';
import { mk } from '../components/controls';
// Native DOM controller: this feature exclusively owns its static Vue subtree.

export const domState = {};
domState.$ = function (s) { return document.querySelector(s); };

export function toast(msg, kind) {
    var t = domState.$("#toast");
    t.textContent = msg;
    t.className = "toast show " + (kind || "");
    // 错误必须能被读到：切到 alert 且不自动消失（点击关闭）。
    t.setAttribute("role", kind === "err" ? "alert" : "status");
    t.setAttribute("aria-live", kind === "err" ? "assertive" : "polite");
    clearTimeout(domState.toastTimer);
    if (kind !== "err") {
        domState.toastTimer = setTimeout(function () { t.className = "toast"; }, 4600);
    }
}

export function legacyCopy(text) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.className = "sr-only";
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try {
        ok = document.execCommand("copy");
    }
    catch (e) {
        ok = false;
    }
    document.body.removeChild(ta);
    return ok;
}

// copyText 优先用异步剪贴板 API（127.0.0.1 属于安全上下文），
// 失败再回退到已废弃的 execCommand。
export function copyText(text, okMsg) {
    var done = function (ok) {
        toast(ok ? (okMsg || "已复制") : "复制失败，请手动选择后按 Ctrl+C", ok ? "ok" : "err");
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
        return navigator.clipboard.writeText(text).then(function () { done(true); }, function () { done(legacyCopy(text)); });
    }
    done(legacyCopy(text));
    return Promise.resolve();
}

export function fmtTime(s) {
    if (!s)
        return "—";
    var d = new Date(s * 1000);
    var p = function (n) { return (n < 10 ? "0" : "") + n; };
    return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds());
}

export function fmtDur(sec) {
    if (sec === undefined || sec === null)
        return "—";
    if (sec <= 0)
        return "已过期";
    var h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60);
    if (h > 0)
        return h + " 小时 " + m + " 分";
    return m + " 分 " + (sec % 60) + " 秒";
}

export function fmtNum(n) {
    if (n === undefined || n === null)
        return "—";
    return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

// fmtShort 给 KPI 卡片用的紧凑时间：10-16 08:19（完整值放在 title 里）
export function fmtShort(s) {
    if (!s)
        return "—";
    var d = new Date(s * 1000);
    var p = function (n) { return (n < 10 ? "0" : "") + n; };
    return (d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
}

export function setHealth(cls, text) {
    var el = domState.$("#health");
    el.className = "badge " + cls;
    el.title = text;
    el.textContent = "";
    el.appendChild(mk("i", "dot"));
    el.appendChild(mk("span", null, text));
}

export function mountDom() {
"use strict";
document.querySelectorAll("[data-select-on-click]").forEach(function (el) {
    listen(el, "click", function () { this.select(); });
});
listen(domState.$("#toast"), "click", function () { this.className = "toast"; });
}

export function disposeToast() { clearTimeout(domState.toastTimer); }
