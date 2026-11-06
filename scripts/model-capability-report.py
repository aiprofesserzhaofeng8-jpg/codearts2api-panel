#!/usr/bin/env python3
"""Render existing audit evidence as Markdown, CSV and a searchable offline HTML."""
import argparse
import collections
import csv
import json
from pathlib import Path

FEATURES = {
    "chat_text": "Chat 文本", "chat_stream": "Chat 流式",
    "responses_text": "Responses 文本", "responses_stream": "Responses 流式",
    "messages_text": "Messages 文本", "messages_stream": "Messages 流式",
    "chat_tools": "Chat 工具", "responses_tools": "Responses 工具",
    "messages_tools": "Messages 工具", "image": "图片",
    "image_tool_result": "工具读图", "json_schema": "JSON 样例",
}


def verdict(case):
    status = case.get("status")
    if status == "PASS":
        return "通过"
    if status == "BLOCKED":
        return "受阻未验"
    if status == "ERROR":
        return "测试异常"
    req = case.get("requests") or []
    codes = [r.get("status") for r in req]
    detail = str(case.get("error") or "") + json.dumps(req, ensure_ascii=False)
    if "该密钥已触发网关限流" in detail:
        return "网关限流"
    if any(code == 429 for code in codes):
        return "上游限流"
    if "原生 Messages 思考与签名" in detail:
        return "历史不兼容"
    if "model_capability_unavailable" in detail:
        return "能力门禁"
    if "not multimodal" in detail:
        return "上游不支持图片"
    if "暂不支持" in detail or "尚不支持" in detail or "not supported" in detail or "unsupported" in detail:
        return "接口拒绝"
    if any(code == 503 for code in codes):
        return "暂无来源"
    if any(code in (401, 402, 403) for code in codes):
        return "鉴权或配额"
    if any(code in (0, 502, 504) for code in codes):
        return "上游或网络"
    if any(r.get("error") for r in req):
        return "网络或断流"
    if any(r.get("finish") in ("length", "max_tokens", "incomplete") for r in req):
        return "输出截断"
    if len(req) > 1 and req[-1].get("tool_calls") and not req[-1].get("answer"):
        return "工具未闭环"
    if codes and codes[-1] == 200:
        if case.get("checks", {}).get("answer") and not case.get("checks", {}).get("usage", True):
            return "用量缺失"
        return "内容未通过"
    return "请求未通过"


def load(root):
    rows = []
    for scope, folder in (("公开名称", "public"), ("原始来源", "sources")):
        file = root / folder / "results.json"
        if not file.exists():
            raise SystemExit("Missing finished evidence: " + str(file))
        for row in json.loads(file.read_text())["models"]:
            if set(row.get("cases", {})) != set(FEATURES):
                raise SystemExit("Incomplete model evidence: " + row["model"])
            rows.append({**row, "scope": scope, "verdicts": {f: verdict(row["cases"][f]) for f in FEATURES}})
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_dir", type=Path)
    args = parser.parse_args()
    root = args.run_dir.resolve()
    rows = load(root)
    fields = list(FEATURES)
    with (root / "matrix.csv").open("w", newline="", encoding="utf-8-sig") as file:
        writer = csv.writer(file)
        writer.writerow(["范围", "模型", *FEATURES.values()])
        for row in rows:
            name = row["model"]
            if name.startswith(("=", "+", "-", "@")):
                name = "'" + name
            writer.writerow([row["scope"], name, *(row["verdicts"][f] for f in fields)])
    totals = {}
    for scope in ("公开名称", "原始来源"):
        subset = [r for r in rows if r["scope"] == scope]
        totals[scope] = {"models": len(subset), "features": {f: dict(collections.Counter(r["verdicts"][f] for r in subset)) for f in fields}}
    (root / "totals.json").write_text(json.dumps(totals, ensure_ascii=False, indent=2) + "\n")
    observed = []
    for row in rows:
        if row["scope"] != "原始来源":
            continue
        image_cases = [row["cases"][f] for f in ("image", "image_tool_result")]
        image = "supported" if any(c["status"] == "PASS" for c in image_cases) else "unsupported" if any("not multimodal" in json.dumps(c) for c in image_cases) else "unknown"
        observed.append({"source": row["model"], "image_input": image, "chat_tool_calling": "supported" if row["cases"]["chat_tools"]["status"] == "PASS" else "unknown", "checked_at": row.get("finished_at"), "basis": "this run and its account; not a guarantee for future upstream revisions"})
    (root / "observed-capabilities.json").write_text(json.dumps({"applied_to_configuration": False, "sources": observed}, ensure_ascii=False, indent=2) + "\n")
    before = root / "runtime-before.json"
    definitions = json.loads(before.read_text()).get("unified_models", []) if before.exists() else []
    public = {r["model"]: r for r in rows if r["scope"] == "公开名称"}
    source = {r["model"]: r for r in rows if r["scope"] == "原始来源"}
    gaps = []
    for model in definitions:
        row = public.get(model["name"])
        if not row:
            continue
        for capability, feature in (("tools", "chat_tools"), ("images", "image")):
            if row["verdicts"][feature] != "能力门禁":
                continue
            good = [s["id"] for s in model["sources"] if s.get("enabled") and s["id"] in source and source[s["id"]]["cases"][feature]["status"] == "PASS"]
            if good:
                gaps.append({"public_model": model["name"], "capability": capability, "verified_raw_sources": good})
    (root / "capability-gaps.json").write_text(json.dumps(gaps, ensure_ascii=False, indent=2) + "\n")
    lines = ["# 全模型能力验收", "", "本报告保留真实失败，不把目录存在、HTTP 200 或一次回答替代端到端成功。受阻未验表示初始调用不可用后未重复消耗额度，不代表该模型不支持该能力。", "", "JSON 样例通过仅证明接口接受参数且本次返回符合 schema，尚未证明服务端严格执行约束。图片仅测试合成 PNG/base64；工具采用测试侧安全的虚拟读取器，没有执行任意命令。", "", "## 覆盖范围", ""]
    for scope, data in totals.items():
        lines.append(f"- {scope}：{data['models']} 个 × {len(fields)} 项。")
    lines += ["", "## 能力计数", "", "| 项目 | 公开名称通过 | 原始来源通过 |", "| --- | ---: | ---: |"]
    for f in fields:
        lines.append(f"| {FEATURES[f]} | {totals['公开名称']['features'][f].get('通过', 0)} | {totals['原始来源']['features'][f].get('通过', 0)} |")
    lines += ["", "## 逐模型结果", "", "能力门禁表示共享路由缺少明确支持的来源；底层是否支持，需要结合原始来源结果判断。", "", "| 范围 | 模型 | " + " | ".join(FEATURES.values()) + " |", "| --- | --- | " + " | ".join("---" for _ in fields) + " |"]
    for row in rows:
        lines.append("| " + " | ".join([row["scope"], "`" + row["model"] + "`", *(row["verdicts"][f] for f in fields)]) + " |")
    lines += ["", "原始 JSON 在 public/results.json、sources/results.json；首次限流与不兼容参数尝试单独保留。可搜索/筛选的界面见 report.html，表格见 matrix.csv。", ""]
    (root / "MATRIX.md").write_text("\n".join(lines))
    # Only sanitized synthetic evidence is embedded. Render all values with textContent.
    data = json.dumps(rows, ensure_ascii=False).replace("<", "\\u003c").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    feature_json = json.dumps(FEATURES, ensure_ascii=False)
    template = r'''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>模型能力实测</title>
<meta name="viewport" content="width=device-width,initial-scale=1"><style>
body{font:14px system-ui,sans-serif;margin:24px;color:#172b4d;background:#f6f8fb}h1{font-size:24px}p{line-height:1.7}input,select{font:inherit;padding:9px;margin:0 8px 12px 0;border:1px solid #b9c3d0;border-radius:6px}input{width:330px}.wrap{overflow:auto;max-height:72vh;background:white;border:1px solid #d5dce5}table{border-collapse:collapse;width:100%;white-space:nowrap}th,td{padding:10px;border-bottom:1px solid #e4e9ef;text-align:left}th{position:sticky;top:0;background:#e8eef6;z-index:1}td.good{color:#087d45;background:#ecfdf3}td.bad{color:#a82232;background:#fff2f2}td.neutral{color:#627083;background:#f3f4f6}td.caution{color:#935600;background:#fff8e8}tr:hover td{outline:1px solid #c6d4e5}button{font:inherit;cursor:pointer}dialog{border:1px solid #c4cfdd;border-radius:10px;max-width:1000px;max-height:85vh;width:90vw}pre{white-space:pre-wrap;word-break:break-word;line-height:1.5}.hint{color:#58687c}#count{margin:10px 0}
</style><h1>模型能力实测</h1><p>覆盖每个公开名称及配置来源。点击单元格查看 HTTP、工具、结束原因和错误证据。<br>“能力门禁”不能直接解读为底层模型不支持。“JSON 样例”不代表已证明服务端强制约束。</p>
<input id="search" placeholder="搜索名称、平台或错误"><select id="scope"><option value="">全部范围</option><option>公开名称</option><option>原始来源</option></select><select id="feature"><option value="">筛选能力</option></select><select id="state"><option value="">全部结果</option><option>通过</option><option>能力门禁</option><option>受阻未验</option><option>上游或网络</option><option>内容未通过</option><option>输出截断</option><option>上游限流</option></select><div id="count"></div><div class="wrap"><table><thead><tr id="head"></tr></thead><tbody id="body"></tbody></table></div>
<dialog id="details"><button id="close">关闭</button><pre id="evidence"></pre></dialog><p class="hint">只使用合成测试数据，不包含请求密钥。运行快照不会修改模型配置或生产程序；真实请求会更新正常日志、用量及健康状态。</p>
<script>const rows=__DATA__, features=__FEATURES__;
const $=id=>document.getElementById(id),head=$('head');for(const label of ['范围','模型',...Object.values(features)]){const th=document.createElement('th');th.textContent=label;head.append(th)}
for(const [key,label] of Object.entries(features)){const op=document.createElement('option');op.value=key;op.textContent=label;$('feature').append(op)}
const knownStates=new Set([...$('state').options].map(o=>o.value));for(const status of new Set(rows.flatMap(r=>Object.values(r.verdicts)))){if(knownStates.has(status))continue;const op=document.createElement('option');op.value=status;op.textContent=status;$('state').append(op)}
function show(row,key){$('evidence').textContent=row.model+' / '+features[key]+'\n'+JSON.stringify(row.cases[key],null,2);$('details').showModal()}
function draw(){const query=$('search').value.toLowerCase(),scope=$('scope').value,feature=$('feature').value,state=$('state').value;const filtered=rows.filter(r=>(!scope||r.scope===scope)&&(!query||JSON.stringify(r).toLowerCase().includes(query))&&(!state||(feature?r.verdicts[feature]===state:Object.values(r.verdicts).includes(state))));$('count').textContent='显示 '+filtered.length+' / '+rows.length+' 项';$('body').replaceChildren();for(const r of filtered){const tr=document.createElement('tr');for(const text of [r.scope,r.model]){const td=document.createElement('td');td.textContent=text;tr.append(td)}for(const key of Object.keys(features)){const td=document.createElement('td'),status=r.verdicts[key];td.textContent=status;td.className=status==='通过'?'good':status==='受阻未验'?'neutral':['能力门禁','接口拒绝','暂无来源','上游限流'].includes(status)?'caution':'bad';td.style.cursor='pointer';td.onclick=()=>show(r,key);tr.append(td)}$('body').append(tr)}}
for(const id of ['search','scope','feature','state'])$(id).addEventListener('input',draw);$('close').onclick=()=>$('details').close();draw();</script></html>'''
    template = template.replace("__DATA__", data).replace("__FEATURES__", feature_json)
    (root / "report.html").write_text(template)
    print(json.dumps(totals, ensure_ascii=False))


if __name__ == "__main__":
    main()
