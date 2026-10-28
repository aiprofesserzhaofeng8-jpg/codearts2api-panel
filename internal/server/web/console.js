(function(){
  "use strict";
  var $ = function(s){ return document.querySelector(s); };
  var state = { data:null };
  var accountFilter = "all";
  function updateThemeLabel(){
    var light = document.documentElement.dataset.theme === "light";
    $("#themeLabel").textContent = light ? "深色" : "浅色";
    $("#themeToggle").title = light ? "切换到深色主题" : "切换到浅色主题";
  }
  var localPanelMeta = document.querySelector('meta[name="local-panel-session"]');
  var localPanelSession = localPanelMeta ? localPanelMeta.content : "";
  // 设置表单是否被用户改过：改过之后 refresh() 不得用服务端值覆盖。
  var settingsDirty = false;

  function toast(msg, kind){
    var t = $("#toast");
    t.textContent = msg;
    t.className = "toast show " + (kind||"");
    // 错误必须能被读到：切到 alert 且不自动消失（点击关闭）。
    t.setAttribute("role", kind === "err" ? "alert" : "status");
    t.setAttribute("aria-live", kind === "err" ? "assertive" : "polite");
    clearTimeout(toast._t);
    if (kind !== "err") {
      toast._t = setTimeout(function(){ t.className = "toast"; }, 4600);
    }
  }
  var adminAuthenticated = false;
  function pw(){ return $("#pw").value; }
  function clearStoredPw(){
    try { localStorage.removeItem("codearts2api_pw"); sessionStorage.removeItem("codearts2api_pw"); } catch(e){}
  }
  function showAdminLogin(){
    adminAuthenticated = false;
    document.body.classList.add("admin-locked");
    if (!$("#adminLoginDialog").open) $("#adminLoginDialog").showModal();
    updatePwToggle();
  }
  function adminLogin(password){
    return api("/admin/auth/login",{method:"POST",body:{password:password}}).then(function(){
      adminAuthenticated = true; clearStoredPw(); $("#pw").value="";
      $("#adminLoginError").textContent=""; $("#adminLoginDialog").close();
      document.body.classList.remove("admin-locked"); updatePwToggle();
    });
  }
  function api(path, opts){
    opts = opts || {};
    var h = { "Content-Type":"application/json" };
    if (localPanelSession) h["X-Local-Panel-Session"] = localPanelSession;

    return fetch(path, {
      method: opts.method || "GET",
      headers: h,
      body: opts.body ? JSON.stringify(opts.body) : undefined
    }).then(function(r){
      return r.text().then(function(txt){
        var j = null;
        try { j = JSON.parse(txt); } catch(e){}
        if (!r.ok) {
          var m = (j && j.error && j.error.message) || txt || ("HTTP " + r.status);
          if (r.status === 401 && path.indexOf("/admin/api/") === 0) showAdminLogin();
          var err = new Error(m); err.status = r.status; throw err;
        }
        return j;
      });
    });
  }

  // --- DOM 构建助手（全程 textContent，绝不拼接 HTML） ---
  function mk(tag, cls, text){
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined && text !== null) n.textContent = String(text);
    return n;
  }
  function cell(text, cls){ return mk("td", cls, text); }
  function row(cells){
    var r = mk("tr");
    for (var i = 0; i < cells.length; i++) r.appendChild(cells[i]);
    return r;
  }
  function pill(text, cls){ return mk("span", "badge " + (cls||""), text); }
  function emptyRow(cols, text){
    var c = cell(text, "empty");
    c.colSpan = cols;
    var r = mk("tr");
    r.appendChild(c);
    return r;
  }
  function btn(label, cls, fn){
    var b = mk("button", "btn " + (cls||""), label);
    b.type = "button";
    b.onclick = busyable(b, fn);
    return b;
  }

  // busyable 在异步操作期间禁用按钮，避免双击造成重复提交
  // （重复签到 / 重复创建密钥 / 重复删除）。
  function busyable(el, fn){
    return function(ev){
      if (el && el.disabled) return;
      var r = fn.call(this, ev);
      if (el && r && typeof r.then === "function") {
        el.disabled = true;
        el.setAttribute("aria-busy", "true");
        r.then(function(){}, function(){}).then(function(){
          el.disabled = false;
          el.removeAttribute("aria-busy");
        });
      }
      return r;
    };
  }
  // on 给静态按钮绑定处理器并自动加 in-flight 保护。
  function on(id, fn){
    var el = $(id);
    el.onclick = busyable(el, fn);
    return el;
  }

  function legacyCopy(text){
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.className = "sr-only";
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try { ok = document.execCommand("copy"); } catch(e){ ok = false; }
    document.body.removeChild(ta);
    return ok;
  }
  // copyText 优先用异步剪贴板 API（127.0.0.1 属于安全上下文），
  // 失败再回退到已废弃的 execCommand。
  function copyText(text, okMsg){
    var done = function(ok){
      toast(ok ? (okMsg || "已复制") : "复制失败，请手动选择后按 Ctrl+C", ok ? "ok" : "err");
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text).then(
        function(){ done(true); },
        function(){ done(legacyCopy(text)); });
    }
    done(legacyCopy(text));
    return Promise.resolve();
  }

  function fmtTime(s){
    if (!s) return "—";
    var d = new Date(s * 1000);
    var p = function(n){ return (n<10?"0":"")+n; };
    return d.getFullYear()+"-"+p(d.getMonth()+1)+"-"+p(d.getDate())+" "+p(d.getHours())+":"+p(d.getMinutes())+":"+p(d.getSeconds());
  }
  function fmtDur(sec){
    if (sec === undefined || sec === null) return "—";
    if (sec <= 0) return "已过期";
    var h = Math.floor(sec/3600), m = Math.floor((sec%3600)/60);
    if (h > 0) return h + " 小时 " + m + " 分";
    return m + " 分 " + (sec%60) + " 秒";
  }
  function fmtNum(n){
    if (n === undefined || n === null) return "—";
    return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  }
  // fmtShort 给 KPI 卡片用的紧凑时间：10-16 08:19（完整值放在 title 里）
  function fmtShort(s){
    if (!s) return "—";
    var d = new Date(s * 1000);
    var p = function(n){ return (n < 10 ? "0" : "") + n; };
    return (d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
  }

  function setHealth(cls, text){
    var el = $("#health");
    el.className = "badge " + cls;
    el.title = text;
    el.textContent = "";
    el.appendChild(mk("i", "dot"));
    el.appendChild(mk("span", null, text));
  }

  function loadHealth(){
    fetch("/health").then(function(r){ return r.json(); }).then(function(h){
      var ready = h.ready || 0, total = h.accounts || 0;
      var ok = total > 0 && ready > 0;
      setHealth(ok ? "ok" : (total === 0 ? "warn" : "err"),
        "账号 "+ready+"/"+total+" 就绪 · 模型 "+(h.models||0));
    }).catch(function(){ setHealth("err", "不可达"); });
  }

  function usageNumber(n){
    n = Number(n || 0);
    if (n >= 1000000) return (n / 1000000).toFixed(2).replace(/\.00$/, "") + "M";
    if (n >= 1000) return (n / 1000).toFixed(2).replace(/\.00$/, "") + "K";
    return n.toLocaleString();
  }
  function usageIcon(kind){
    var paths = {
      requests:["M7 3h7l4 4v14H6V3h1Z", "M14 3v5h4M9 12h6M9 16h6"],
      tokens:["m12 3 9 5-9 5-9-5 9-5Z", "M3 8v9l9 5 9-5V8M12 13v9"],
      cost:["M15 8h-4a2 2 0 0 0 0 4h2a2 2 0 0 1 0 4H9M12 6v12"],
      latency:["M12 6v6h4"]
    };
    var icon = mk("span", "usage-icon " + kind);
    var ns = document.querySelector("nav svg").namespaceURI, svg = document.createElementNS(ns,"svg");
    svg.setAttribute("viewBox","0 0 24 24");svg.setAttribute("aria-hidden","true");
    if (kind === "cost" || kind === "latency") {
      var circle=document.createElementNS(ns,"circle");circle.setAttribute("cx","12");circle.setAttribute("cy","12");circle.setAttribute("r","9");svg.appendChild(circle);
    }
    paths[kind].forEach(function(d){var path=document.createElementNS(ns,"path");path.setAttribute("d",d);svg.appendChild(path);});
    icon.appendChild(svg);
    return icon;
  }
  function money(amount,currency){return currency === "USD" ? "$" + Number(amount || 0).toFixed(6) : "—";}
  function renderUsage(s){
    var known = Number(s.usage_requests || 0), input = Number(s.input_tokens || 0), output = Number(s.output_tokens || 0);
    var cached = Math.min(input, Number(s.cached_tokens || 0)), total = input + output;
    var samples = Number(s.latency_samples || 0), requests = Number(s.total_requests || 0), failed = Number(s.failed_requests || 0);
    var measured = function(n){ return known ? usageNumber(n) : "—"; };
    var amounts=[];
    if (s.priced_usd_requests) amounts.push(money(s.estimated_usd,"USD"));
    var cards = [
      {kind:"requests", label:"总请求数", value:usageNumber(requests), sub:"所选范围内 · 失败 " + usageNumber(failed)},
      {kind:"tokens", label:"总 Token", value:measured(total), sub:"输入: " + measured(input-cached) + " / 输出: " + measured(output) + " / 缓存: " + measured(cached)},
      {kind:"cost", label:"API 估算费用", value:amounts.join(" + ") || "—", sub:"价格表估算 · 已计价 " + (s.priced_requests || 0) + "/" + requests + " 次；非华为账单"},
      {kind:"latency", label:"平均耗时", value:samples ? (s.total_latency_ms / samples / 1000).toFixed(2) + "s" : "—", sub:samples ? "基于 " + usageNumber(samples) + " 次请求" : "等待新请求产生耗时数据"}
    ];
    var box = $("#kpis"); box.textContent = "";
    cards.forEach(function(item){
      var card = mk("div", "usage-kpi " + item.kind); card.appendChild(usageIcon(item.kind));
      var content = mk("div", "usage-kpi-content");
      content.appendChild(mk("div", "usage-label", item.label));
      content.appendChild(mk("div", "usage-value", item.value));
      content.appendChild(mk("div", "usage-sub", item.sub));
      card.appendChild(content); box.appendChild(card);
    });
    $("#usagePeriod").textContent = "全部时间 · 累计模型请求";
    $("#usageCoverage").textContent = "Token 已覆盖 " + known.toLocaleString() + " / " + requests.toLocaleString() + " 次请求。" +
      (known < requests ? "历史或上游未提供的用量显示为 —，不按字符数估算。" : "缓存包含在输入 Token 中，推理包含在输出 Token 中。") +
      "";
    if(s.partial_usage_requests) $("#usageCoverage").textContent+=" 有 "+s.partial_usage_requests+" 次请求的尝试用量不完整，统计与估价仅计已报告部分。";
    var breakdown = $("#usageBreakdown"); breakdown.textContent = "";
    [{label:"输入（未缓存）",value:input-cached,tone:"input"},{label:"输出",value:output,tone:"output"},{label:"缓存命中",value:cached,tone:"cached"}].forEach(function(item){
      var line = mk("div", "usage-token-row " + item.tone), title = mk("div", "usage-token-label");
      title.appendChild(mk("span", "", item.label)); title.appendChild(mk("strong", "", measured(item.value)));
      line.appendChild(title);
      var meter = document.createElement("progress"); meter.max = total || 1; meter.value = item.value;
      meter.setAttribute("aria-label", item.label); line.appendChild(meter); breakdown.appendChild(line);
    });
    var health = $("#usageHealth"); health.textContent = "";
    [{label:"成功请求",value:usageNumber(Math.max(0,requests-failed)),tone:"good"},{label:"失败请求",value:usageNumber(failed),tone:"bad"},
     {label:"成功率",value:requests ? ((requests-failed)/requests*100).toFixed(1)+"%" : "—",tone:""},{label:"摘要 / 失败 / 重试",value:(s.summary_requests || 0)+" / "+(s.summary_failures || 0)+" / "+(s.summary_retries || 0),tone:""},{label:"最终截断",value:usageNumber(s.truncated_requests || 0),tone:""}].forEach(function(item){
      var line=mk("div","usage-result "+item.tone);line.appendChild(mk("span","",item.label));line.appendChild(mk("strong","",item.value));health.appendChild(line);
    });
  }
  var usageView={page:1,pageSize:20,start:0,end:0,label:"全部已保存记录",pages:1,total:0,busy:false,seq:0};
  function localDate(d){return d.getFullYear()+"-"+String(d.getMonth()+1).padStart(2,"0")+"-"+String(d.getDate()).padStart(2,"0");}
  function setUsagePreset(){
    var preset=$("#usagePreset").value, start=new Date(), end=new Date();
    if (preset === "custom") return;
    if (preset === "all") {$("#usageStart").value="";$("#usageEnd").value="";}
    else {
      if (preset === "yesterday") {start.setDate(start.getDate()-1);end.setDate(end.getDate()-1);}
      if (preset === "7" || preset === "30") start.setDate(start.getDate()-Number(preset)+1);
      if (preset === "month") start.setDate(1);
      if (preset === "lastMonth") {start=new Date(start.getFullYear(),start.getMonth()-1,1);end=new Date(end.getFullYear(),end.getMonth(),0);}
      $("#usageStart").value=localDate(start);$("#usageEnd").value=localDate(end);
    }
    applyUsageDates();
  }
  function applyUsageDates(){
    var a=$("#usageStart").value,b=$("#usageEnd").value;
    if ((!a)!==(!b) || a>b) {toast("请选择有效的开始和结束日期","err");return;}
    var start=a ? new Date(a+"T00:00:00") : null,end=b ? new Date(b+"T00:00:00") : null;
    if ((start&&!Number.isFinite(start.getTime()))||(end&&!Number.isFinite(end.getTime()))) {toast("日期格式无效","err");return;}
    if (end) end.setDate(end.getDate()+1);
    usageView.start=start?Math.floor(start.getTime()/1000):0;usageView.end=end?Math.floor(end.getTime()/1000):0;
    usageView.label=a ? a+" 至 "+b : "全部已保存记录";usageView.page=1;
    return refreshUsageLogs();
  }
  function renderUsagePagination(){
    var v=usageView,first=v.total?(v.page-1)*v.pageSize+1:0,last=Math.min(v.total,v.page*v.pageSize);
    $("#usagePageInfo").textContent="显示 "+first+"–"+last+" 条，共 "+v.total+" 条";
    $("#usagePageSize").value=String(v.pageSize);$("#usagePageJump").max=String(v.pages);$("#usagePageJump").value=String(v.page);
    var box=$("#usagePageButtons");box.textContent="";
    function pageButton(label,page,current){
      var button=btn(label,"xs"+(current?" primary":""),function(){usageView.page=page;return refreshUsageLogs();});
      button.disabled=v.busy || page<1 || page>v.pages || current;
      if (current) button.setAttribute("aria-current","page");
      button.setAttribute("aria-label",label === "上一页" || label === "下一页" ? label : "第 "+page+" 页");box.appendChild(button);
    }
    pageButton("上一页",v.page-1,false);
    var pages=[1];for(var n=Math.max(2,v.page-2);n<=Math.min(v.pages-1,v.page+2);n++) pages.push(n);
    if(v.pages>1) pages.push(v.pages);
    pages.forEach(function(n,i){if(i&&n-pages[i-1]>1)box.appendChild(mk("span","pagination-ellipsis","…"));pageButton(String(n),n,n===v.page);});
    pageButton("下一页",v.page+1,false);
    $("#usageJump").disabled=v.busy;$("#usagePageSize").disabled=v.busy;
  }
  function jumpUsagePage(){
    var n=Number($("#usagePageJump").value);
    if(!Number.isInteger(n)||n<1||n>usageView.pages){toast("请输入 1 至 "+usageView.pages+" 的页码","err");return;}
    usageView.page=n;return refreshUsageLogs();
  }
  function refreshUsageLogs(){
    var seq=++usageView.seq;usageView.busy=true;renderUsagePagination();
    $("#usageLogs").setAttribute("aria-busy","true");
    var query=new URLSearchParams({page:String(usageView.page),page_size:String(usageView.pageSize)});
    if($("#usageProvider").value)query.set("provider",$("#usageProvider").value);
    if(usageView.start)query.set("start",String(usageView.start));if(usageView.end)query.set("end",String(usageView.end));
    return api("/admin/api/usage?"+query.toString()).then(function(d){
      if(seq!==usageView.seq)return;
      usageView.page=d.page;usageView.pages=d.pages;usageView.total=d.total;
      renderUsage(d.stats || {});
      $("#usagePeriod").textContent=usageView.label+" · 所选范围内";
      $("#usageHistoryHint").textContent="已保存 "+d.retained+" 条用量记录"+(d.oldest?"，最早 "+fmtTime(d.oldest):"")+"。最多保留 "+d.retention_limit+" 条；升级前已清理的记录无法恢复。";
      var tb=$("#usageLogs");tb.textContent="";
      var logs=d.logs || [];
      if(!logs.length){tb.appendChild(emptyRow(10,"所选时间范围内暂无请求记录"));return;}
      logs.forEach(function(l){
        var u=l.usage || {},known=u.known,input=Number(u.input_tokens || 0),output=Number(u.output_tokens || 0),cached=Math.min(input,Number(u.cached_tokens || 0));
        var number=function(n){return known?(l.usage_incomplete?"≥":"")+usageNumber(n):"—";};
        var status=cell("");status.appendChild(pill(l.status,l.status>=400?"err":"ok"));
        if(l.finish_reason)status.appendChild(mk("span","usage-finish",l.finish_reason));
        if(l.request_id)status.title="请求 ID: "+l.request_id;
        if(l.err)status.title=(status.title || "")+" · "+l.err;
        tb.appendChild(row([cell(fmtTime(l.time),"muted"),cell(l.model,"mono"),cell((l.protocol || "—")+(l.request_kind === "summary" ? " · 摘要 ×"+(l.summary_attempts || 1) : ""),"muted"),status,
          cell(number(input-cached),"mono"),cell(number(output),"mono"),cell(number(cached),"mono"),cell(number(input+output),"mono"),cell(l.estimated_cost?money(l.estimated_cost.amount,l.estimated_cost.currency)+(l.estimated_cost.partial?"（部分）":""):"—","mono"),cell((l.latency_ms/1000).toFixed(2)+"s","mono")]));
      });
    }).catch(function(e){
      if(seq!==usageView.seq)return;
      renderUsage({});usageView.page=1;usageView.pages=1;usageView.total=0;
      $("#usagePeriod").textContent=usageView.label+" · 加载失败，请重试";
      $("#usageLogs").textContent="";$("#usageLogs").appendChild(emptyRow(10,"用量加载失败："+e.message));
    }).finally(function(){if(seq===usageView.seq){usageView.busy=false;renderUsagePagination();$("#usageLogs").setAttribute("aria-busy","false");}});
  }

  var priceCache={},priceEditing=null,priceSaving=false;
  function renderPricing(){
    var ids=Object.keys(priceCache);
    ((state.data || {}).models || []).forEach(function(m){if(m.source!=="alias"&&ids.indexOf(m.id)<0)ids.push(m.id);});
    var tb=$("#pricingRows");tb.textContent="";
    ids.sort().forEach(function(id){
      var p=priceCache[id],ops=cell("");ops.appendChild(btn("编辑","xs",function(){openPriceEditor(id);}));
      tb.appendChild(row([cell(id,"mono"),cell("USD","mono"),cell(p?String(p.input):"未配置","mono"),cell(p?String(p.output):"未配置","mono"),cell(p?String(p.cached):"未配置","mono"),cell(p?p.source+" · "+p.updated_at:"未核实官方单价，可自定义","muted pricing-source"),ops]));
    });
    if(!ids.length)tb.appendChild(emptyRow(7,"暂无模型，请先刷新模型目录"));
  }
  function loadPricing(){
    if(!state.data)return Promise.resolve();
    return api("/admin/api/pricing").then(function(d){priceCache=d.prices || {};renderPricing();}).catch(function(e){toast("价格表加载失败："+e.message,"err");});
  }
  function openPriceEditor(id){
    if(priceSaving)return;
    priceEditing=id;var p=priceCache[id];
    $("#priceEditModel").textContent=id;$("#priceEditError").textContent="";
    $("#priceEditInput").value=p?p.input:"";$("#priceEditOutput").value=p?p.output:"";$("#priceEditCached").value=p?p.cached:"";
    $("#priceEditDialog").showModal();$("#priceEditInput").focus();
  }
  function closePriceEditor(){if(!priceSaving){$("#priceEditDialog").close();priceEditing=null;}}
  function savePriceEditor(){
    if(priceSaving || !priceEditing)return;
    var price={currency:"USD",source:"管理员自定义参考价"},fields={input:"#priceEditInput",output:"#priceEditOutput",cached:"#priceEditCached"};
    for(var field in fields){var input=$(fields[field]);if(input.value.trim()===""||!input.checkValidity()){$("#priceEditError").textContent="请填写三项有效单价，0 表示免费";input.focus();return;}price[field]=Number(input.value);}
    priceSaving=true;$("#priceEditError").textContent="";
    ["#priceEditSave","#priceEditCancel","#priceEditClose","#priceEditInput","#priceEditOutput","#priceEditCached"].forEach(function(id){$(id).disabled=true;});
    return api("/admin/api/pricing",{method:"POST",body:{model:priceEditing,price:price}}).then(function(result){
      priceCache=result.prices;renderPricing();$("#priceEditDialog").close();priceEditing=null;toast("价格已保存，仅影响新请求","ok");
    }).catch(function(e){$("#priceEditError").textContent="保存失败："+e.message;}).finally(function(){priceSaving=false;["#priceEditSave","#priceEditCancel","#priceEditClose","#priceEditInput","#priceEditOutput","#priceEditCached"].forEach(function(id){$(id).disabled=false;});});
  }

  var modelPolicies={},editingPolicy=null,savingPolicy=false;
  var policyFields={context_window:"#policyContext",max_output_tokens:"#policyOutput",reserve_tokens:"#policyReserve",keep_recent_tokens:"#policyKeep",trigger_tokens:"#policyTrigger",summary_max_tokens:"#policySummaryMax"};
  function loadModelPolicies(){
    if(!state.data)return Promise.resolve();
    return api("/admin/api/model-policies").then(function(d){modelPolicies=d.policies || {};renderModelPolicies();}).catch(function(e){toast("策略加载失败："+e.message,"err");});
  }
  function renderModelPolicies(){
    var ids=Object.keys(modelPolicies);((state.data || {}).models || []).forEach(function(m){if(m.source!=="alias"&&ids.indexOf(m.id)<0)ids.push(m.id);});
    var tb=$("#modelPolicies");tb.textContent="";
    ids.sort().forEach(function(id){var p=modelPolicies[id],ops=cell("");ops.appendChild(btn("编辑","xs",function(){openModelPolicy(id);}));tb.appendChild(row([
      cell(id,"mono"),cell(p?p.context_window+" / "+p.max_output_tokens:"未配置","mono"),cell(p?p.trigger_tokens+" / "+p.keep_recent_tokens:"—","mono"),
      cell(p&&p.summary_enabled?(p.summary_retry?"完整性保护 · 最多 2 次":"完整性保护 · 单次"):"关闭","muted"),cell(p?p.source:"先验证模型能力，再配置","muted pricing-source"),ops]));});
    if(!ids.length)tb.appendChild(emptyRow(6,"暂无模型"));
  }
  function openModelPolicy(id){
    if(savingPolicy)return;editingPolicy=id;$("#policyModel").textContent=id;$("#policyError").textContent="";
    var p=modelPolicies[id] || {};Object.keys(policyFields).forEach(function(k){$(policyFields[k]).value=p[k] || "";});
    var fallback=$("#policyFallback");fallback.textContent="";var none=mk("option","","不使用备用模型");none.value="";fallback.appendChild(none);
    ((state.data || {}).models || []).forEach(function(m){if(m.source!=="alias"&&m.id!==id){var o=mk("option","",m.id);o.value=m.id;fallback.appendChild(o);}});
    fallback.value=p.summary_fallback_model || "";
    $("#policySummary").checked=!!p.summary_enabled;$("#policyRetry").checked=!!p.summary_retry;$("#policyThinking").checked=!!p.disable_summary_thinking;
    $("#policyDialog").showModal();
  }
  function closeModelPolicy(){if(!savingPolicy){$("#policyDialog").close();editingPolicy=null;}}
  function saveModelPolicy(){
    if(savingPolicy||!editingPolicy)return;
    var policy={summary_enabled:$("#policySummary").checked,summary_retry:$("#policyRetry").checked,disable_summary_thinking:$("#policyThinking").checked};
    for(var k in policyFields){var input=$(policyFields[k]);if(!input.checkValidity()||input.value.trim()===""){$("#policyError").textContent="请填写有效整数参数";input.focus();return;}policy[k]=Number(input.value);}
    policy.summary_fallback_model=$("#policyFallback").value;
    savingPolicy=true;$("#policyError").textContent="";
    return api("/admin/api/model-policies",{method:"POST",body:{model:editingPolicy,policy:policy}}).then(function(d){modelPolicies=d.policies;renderModelPolicies();$("#policyDialog").close();editingPolicy=null;toast("策略已保存；重新运行客户端启动器获取更新","ok");}).catch(function(e){$("#policyError").textContent=e.message;}).finally(function(){savingPolicy=false;});
  }

  function renderOverview(d){
  	var tb = $("#models");
    tb.textContent = "";
    var models = d.models || [];
    // 每次渲染重建元数据索引：上游清单可能删掉了模型，留着旧条目会让
    // 「测活全部」去探测已经不存在的模型，并让单行重绘拿到 undefined。
    lastModels = {};
    if (!models.length) { tb.appendChild(emptyRow(7, "无")); return; }
    models.forEach(function(m){
      lastModels[m.id] = m;
      tb.appendChild(modelRow(m));
    });
    $("#modelHint").textContent = d.upstream_error ? ("最近一次上游刷新失败：" + d.upstream_error) : "";
  }

  // 每个模型的测活状态：id -> {state:"idle|busy|ok|err", err}
  var modelProbeState = {};
  // 最近一次渲染的模型元数据，供单行重绘时复用
  var lastModels = {};

  function modelRow(m){
    var actionCell = cell("", "actions-cell");
    // 别名只是名字映射，指向的仍是上游真实模型，单独测活没有意义。
    if (m.source === "alias") {
      actionCell.appendChild(mk("span", "hint m-0", "别名"));
      return row([
        cell(m.id, "mono"), cell(m.name),
        cell(m.credit || "—", "mono"),
        cell(m.context ? fmtNum(m.context) : "—", "mono"),
        cell(m.max_output ? fmtNum(m.max_output) : "—", "mono"),
        cell(m.description, "muted"),
        actionCell
      ]);
    }

    var btn = mk("button", "btn xs", "测活");
    var st = modelProbeState[m.id] || { state: "idle" };
    if (st.state === "busy") {
      btn.disabled = true;
      btn.textContent = "测活中…";
    } else if (st.state === "ok" || st.state === "err") {
      btn.textContent = "重测";
    }
    btn.onclick = function(){ probeOne(m.id, m.name || m.id); };

    actionCell.appendChild(btn);
    if (st.state === "ok") {
      actionCell.appendChild(document.createTextNode(" "));
      actionCell.appendChild(pill("可用", "ok"));
    } else if (st.state === "err") {
      actionCell.appendChild(document.createTextNode(" "));
      var bad = pill("不可用", "err");
      bad.title = st.err || "";
      actionCell.appendChild(bad);
    }

    var r = row([
      cell(m.id, "mono"), cell(m.name),
      cell(m.credit || "—", "mono"),
      cell(m.context ? fmtNum(m.context) : "—", "mono"),
      cell(m.max_output ? fmtNum(m.max_output) : "—", "mono"),
      cell(m.description, "muted"),
      actionCell
    ]);
    if (st.state === "err" && st.err) r.title = st.err;
    return r;
  }

  // probeOne 对单个模型测活，只重绘那一行。
  function probeOne(modelID, label){
    if (modelProbeState[modelID] && modelProbeState[modelID].state === "busy") return;
    modelProbeState[modelID] = { state: "busy" };
    redrawModelRow(modelID);
    $("#modelHint").textContent = "正在测活 " + label + "…";

    return api("/admin/api/models/probe", {method:"POST", body:{models:[modelID]}}).then(function(r){
      var row0 = (r.results || [])[0] || {};
      modelProbeState[modelID] = row0.ok
        ? { state: "ok", account: row0.account }
        : { state: "err", err: row0.error || "未知错误", account: row0.account };
      redrawModelRow(modelID);
      $("#modelHint").textContent = row0.ok
        ? (label + " 可用" + (row0.account ? "（账号 " + row0.account + "）" : ""))
        : (label + " 不可用：" + (row0.error || "未知错误"));
    }).catch(function(e){
      modelProbeState[modelID] = { state: "err", err: (e && e.message) || "请求失败" };
      redrawModelRow(modelID);
      $("#modelHint").textContent = label + " 测活失败：" + ((e && e.message) || "未知错误");
    });
  }

  // redrawModelRow 只替换指定模型那一行，避免整表重绘打断其他行。
  function redrawModelRow(modelID){
    var meta = lastModels[modelID];
    if (!meta) return; // 清单已刷新、该模型不在了，无事可做
    var tb = $("#models");
    var rows = tb.children;
    for (var i = 0; i < rows.length; i++) {
      var first = rows[i].firstChild;
      if (first && first.textContent === modelID) {
        tb.replaceChild(modelRow(meta), rows[i]);
        return;
      }
    }
  }

  // 每个账号的积分额度：id -> {loading, ok, credit, error}
  var creditState = {};
  var creditLoading = null;

  // loadCredits 查询账号积分额度（上游统计插件）。
  // 默认走服务端 60 秒缓存；force=true 时带 ?fresh=1 强制回源。
  function loadCredits(force){
    if (creditLoading) return creditLoading;
    var accts = (state.data && state.data.accounts) || [];
    if (!accts.length) return Promise.resolve();
    accts.forEach(function(a){ creditState[a.id] = { loading: true }; });
    if (state.data) renderAccounts(state.data);
    var url = "/admin/api/credits" + (force ? "?fresh=1" : "");
    creditLoading = api(url).then(function(d){
      (d.accounts || []).forEach(function(x){
        creditState[x.id] = x.ok
          ? { loading:false, ok:true, credit:x.credit }
          : { loading:false, ok:false, error:x.error };
      });
      if (state.data) renderAccounts(state.data);
    }).catch(function(e){
      var msg = (e && e.message) || "查询失败";
      accts.forEach(function(a){ creditState[a.id] = { loading:false, ok:false, error:msg }; });
      if (state.data) renderAccounts(state.data);
      toast("查询积分额度失败：" + msg, "err");
    }).then(function(){ creditLoading = null; });
    return creditLoading;
  }

  // creditCell 渲染一个账号的积分额度：剩余/总量 + 进度条 + 重置日期。
  function creditCell(a){
    var c = cell("", "cell-stack");
    var st = creditState[a.id];
    if (!st || st.loading) {
      c.appendChild(mk("span", "muted", st ? "查询中…" : "—"));
      return c;
    }
    if (!st.ok) {
      var bad = mk("span", "muted", "查询失败");
      bad.title = st.error || "";
      c.appendChild(bad);
      return c;
    }
    var info = st.credit || {};
    if (!info.available) {
      var accessDenied = a.status === "error" && String(a.last_err || "").indexOf("TM.00001005") >= 0;
      var none = mk("span", "muted", accessDenied ? "需开通码道权限" : (info.is_token_package ? "Token 套餐" : "额度未返回"));
      none.title = accessDenied ? "最近一次模型调用被上游拒绝：尚未获得此功能的访问权限。请使用该账号到码道官方页面核对开通状态。" : (info.reason || "上游未提供积分数据，不能据此判定没有套餐或余额");
      c.appendChild(none);
      return c;
    }
    var total = Number(info.total) || 0;
    var remain = Number(info.remaining) || 0;
    var used = Number(info.used) || 0;
    c.appendChild(mk("span", "mono", fmtNum(remain) + " / " + fmtNum(total)));

    var bar = mk("div", "bar credit-bar");
    var pct = total > 0 ? Math.max(0, Math.min(100, (remain / total) * 100)) : 0;
    var fill = document.createElement("progress");
    fill.max = 100;
    fill.value = pct;
    fill.setAttribute("aria-label", "剩余积分 " + pct.toFixed(0) + "%");
    if (pct <= 20) bar.classList.add("err");
    else if (pct <= 50) bar.classList.add("warn");
    bar.appendChild(fill);
    c.appendChild(bar);

    var bits = ["已用 " + fmtNum(used)];
    if (info.expiring) bits.push("即将到期 " + fmtNum(info.expiring));
    if (info.reset_date) bits.push("重置 " + info.reset_date);
    if (info.status && info.status !== "active") bits.push(info.status);
    var sub = mk("span", "sub", bits.join(" · "));
    sub.title = (info.package_name || "") + (info.spec_code ? (" / " + info.spec_code) : "");
    c.appendChild(sub);
    return c;
  }

  function renderAccounts(d){
    var tb = $("#accounts");
    tb.textContent = "";
    var accts = d.accounts || [];
    var counts = {ready:0, cooling:0, paused:0, disabled:0, busy:0};
    var inFlight = 0;
    accts.forEach(function(a){
      var status = accountState(a);
      counts[status] = (counts[status] || 0) + 1;
      inFlight += a.pool ? a.pool.in_flight || 0 : 0;
    });
    $("#navAccountCount").textContent = accts.length;
    $("#accountCount").textContent = accts.length + " 个账号";
    var stats = $("#poolStats");
    stats.textContent = "";
    [
      {label:"账号总数", value:accts.length, note:"已添加到账号池"},
      {label:"可调度", value:counts.ready, note:"可接收模型请求", tone:"good"},
      {label:"冷却中", value:counts.cooling, note:"到期后自动恢复", tone:"warning"},
      {label:"暂停 / 停用", value:counts.paused + " / " + counts.disabled, note:"暂不参与请求调度"},
      {label:"在途请求", value:inFlight, note:"处理中 · " + counts.busy + " 个账号已满"},
      {label:"调度策略", value:({least_inflight:"最少并发",round_robin:"轮询",sequential_exhaust:"顺序耗尽"})[((d.settings || {}).pool || {}).strategy] || "—", note:"会话优先复用原账号", text:true}
    ].forEach(function(item){
      var stat = mk("div", "pool-stat");
      if (item.tone) stat.classList.add(item.tone);
      stat.appendChild(mk("div", item.text ? "stat-value text-value" : "stat-value", item.value));
      stat.appendChild(mk("div", "stat-label", item.label));
      stat.appendChild(mk("div", "stat-note", item.note));
      stats.appendChild(stat);
    });
    var query = $("#accountSearch").value.trim().toLowerCase();
    var visible = accts.filter(function(a){
      return (!$("#accountProvider").value || (a.provider || "codearts") === $("#accountProvider").value) && (accountFilter === "all" || accountState(a) === accountFilter) &&
        (!query || [a.name, a.source, sourceName(a.source),a.provider,a.scope].join(" ").toLowerCase().indexOf(query) !== -1);
    });
    $("#accountFilterCount").textContent = visible.length + " / " + accts.length + " 个账号";
    if (!accts.length) {
      tb.appendChild(emptyRow(10, "还没有账号。点击「浏览器登录」，复制链接授权后确认添加。"));
      return;
    }
    if (!visible.length) { tb.appendChild(emptyRow(10, "没有符合条件的账号，请更换筛选条件。")); return; }
    visible.forEach(function(a){
      var statusCell = cell("");
      if (a.pool && a.pool.state !== "ready") {
        var labels = {paused:"已暂停选号", cooling:"冷却中", busy:"并发已满", disabled:"已停用"};
        statusCell.appendChild(pill(labels[a.pool.state] || a.pool.state, "warn"));
        if (a.pool.cooldown_remaining_sec > 0) statusCell.appendChild(mk("div", "sub", "剩余 " + fmtDur(a.pool.cooldown_remaining_sec)));
        if (a.cooldown_reason) statusCell.title = a.cooldown_reason;
      } else if (a.enabled) {
        var okStatus = a.status === "ok";
        statusCell.appendChild(pill(okStatus ? "可用" : "待检查", okStatus ? "ok" : "err"));
      } else {
        statusCell.appendChild(pill("已停用"));
      }
      if (a.has_refresh) statusCell.appendChild(document.createTextNode(" "));
      if (a.has_refresh) statusCell.appendChild(pill("可续期", "ok"));
      if (a.pool) statusCell.appendChild(mk("div", "sub", "在途 " + a.pool.in_flight + " / " + a.pool.max_in_flight));

      var expCell = cell("", "cell-stack");
      if (a.permanent) {
        // 永久 AK/SK：没有过期时间，也不参与续期。
        expCell.appendChild(mk("span", "mono", "永不过期"));
        expCell.appendChild(mk("span", "sub", "IAM AK/SK"));
      } else if (a.expires_at) {
        expCell.appendChild(mk("span", "mono", fmtTime(a.expires_at)));
        var left = mk("span", "sub", fmtDur(a.expires_in));
        if (a.expires_in !== undefined && a.expires_in <= 300) left.classList.add("warn-text");
        expCell.appendChild(left);
      } else {
        expCell.appendChild(mk("span", "mono", "—"));
      }

      var claimCell = cell("");
      if (a.provider && a.provider !== "codearts") {claimCell.appendChild(mk("span","muted","未接入"));} else if (a.claimed_today) {
        claimCell.appendChild(pill("已领", "ok"));
      } else {
        claimCell.appendChild(pill("未领", "warn"));
      }
      if (a.last_claim_msg) claimCell.title = a.last_claim_msg;
      if (a.last_claim_err) claimCell.title = a.last_claim_err;

      var ops = cell("", "actions-cell");
      ops.appendChild(btn("模型健康", "xs", function(){openModelHealth(a);}));
      ops.appendChild(btn("探活", "xs", function(){ checkin(a.id); }));
      if(!a.provider || a.provider==="codearts")ops.appendChild(btn("领福利", "xs", function(){ claim(a.id); }));
      if (a.source === "desktop") ops.appendChild(btn("重读", "xs", function(){ accountOp("reimport", {id:a.id}); }));
      ops.appendChild(btn("导出", "xs", function(){ exportSession(a.id); }));
      ops.appendChild(btn(a.enabled ? "停用" : "启用", "xs", function(){ accountOp("toggle", {id:a.id}); }));
      ops.appendChild(btn(a.paused ? "恢复选号" : "暂停选号", "xs", function(){ accountOp(a.paused ? "resume" : "pause", {id:a.id}); }));
      if (a.failures > 0 || (a.pool && a.pool.cooldown_remaining_sec > 0) || Object.keys(a.model_health || {}).some(function(m){return a.model_health[m].failures>0;})) {
        ops.appendChild(btn("解除冷却 / 清错", "xs", function(){ accountOp("clear-errors", {id:a.id}); }));
      }
      ops.appendChild(btn("删除", "xs danger", function(){
        showDeleteAccount(a);
      }));

      var identity = cell("", "account-identity");
      var who = mk("div", "account-who");
      who.appendChild(mk("span", "account-avatar", Array.from(a.name || "码")[0].toUpperCase()));
      var info = mk("div", "account-name");
      info.appendChild(mk("strong", "account-title", a.name || "未命名账号"));
      info.appendChild(pill(a.provider === "workbuddy" ? "WorkBuddy · "+(a.scope === "global" ? "国际" : "国内") : "华为云码道"));
      info.appendChild(mk("span", "account-caption", a.has_refresh ? "自动续期已就绪" : "临时凭据"));
      who.appendChild(info);
      identity.appendChild(who);
      var r = row([
        identity, cell(sourceName(a.source), "muted"), cell(a.access_key || "—", "mono"),
        expCell, creditCell(a), statusCell, claimCell, cell(a.failures, "mono"),
        cell(fmtTime(a.last_used), "muted nowrap"), ops
      ]);
      r.dataset.state = accountState(a);
      var labels = ["账号", "来源", "AK", "凭据有效期", "积分额度", "状态", "今日领取", "失败次数", "最近使用", "操作"];
      Array.from(r.children).forEach(function(td, i){ td.dataset.label = labels[i]; });
      if (a.last_err) r.title = a.last_err;
      tb.appendChild(r);
    });
  }

  function accountState(a){
    if (!a.enabled) return "disabled";
    if (a.paused) return "paused";
    return a.pool ? a.pool.state : "ready";
  }
  function sourceName(source){ return ({browser:"浏览器登录",desktop:"桌面端导入",manual:"手工录入",session:"会话导入"})[source] || source || "—"; }

  function renderKeys(d){
    var tb = $("#keys");
    tb.textContent = "";
    var keys = d.keys || [];
    if (!keys.length) {
      tb.appendChild(emptyRow(7, "暂无密钥（未签发时 /v1 仅允许本机直连）"));
      return;
    }
    keys.forEach(function(k){
	  var keyCell = cell("", "key-cell");
	  var keyInput = mk("input", "mono key-value");
	  keyInput.type = "password";
	  keyInput.readOnly = true;
	  keyInput.value = k.key;
	  keyInput.setAttribute("aria-label", "API 密钥 " + (k.name || k.key));
	  keyInput.autocomplete = "off";
	  keyInput.addEventListener("click", function(){ keyInput.select(); });
	  keyCell.appendChild(keyInput);
      var st = cell("");
      st.appendChild(pill(k.enabled ? "启用" : "停用", k.enabled ? "ok" : ""));
      var ops = cell("", "actions-cell");
      function fetchKey(){return api("/admin/api/keys/reveal",{method:"POST",body:{key:k.ref || k.key}}).then(function(d){if(!d.value)throw new Error("密钥不存在");return d.value;});}
      ops.appendChild(btn("复制", "xs primary", function(){
        return fetchKey().then(function(value){return copyText(value,"密钥已复制");}).catch(function(e){toast(e.message,"err");});
      }));
      var reveal = btn("显示", "xs", function(){
        if(keyInput.type === "text"){keyInput.type="password";keyInput.value=k.key;reveal.textContent="显示";return;}
        return fetchKey().then(function(value){keyInput.value=value;keyInput.type="text";reveal.textContent="隐藏";}).catch(function(e){toast(e.message,"err");});
      });
      ops.appendChild(reveal);
      ops.appendChild(btn("预算", "xs",function(){openKeyBudget(k);}));
      ops.appendChild(btn(k.enabled ? "停用" : "启用", "xs", function(){ keyOp("toggle", k.ref || k.key); }));
      ops.appendChild(btn("删除", "xs danger", function(){
        if (confirm("确定删除该密钥？")) keyOp("key", k.ref || k.key);
      }));
      tb.appendChild(row([
        keyCell, cell(k.name), st, cell(k.requests, "mono"),
        cell(fmtTime(k.created_at), "muted"), cell(fmtTime(k.last_used), "muted"), ops
      ]));
    });
  }

  function renderLogs(logs){
    var tb = $("#logs");
    tb.textContent = "";
    if (!logs || !logs.length) { tb.appendChild(emptyRow(9, "暂无日志")); return; }
    logs.forEach(function(l){
      var st = cell("");
      st.appendChild(pill(l.status, l.status >= 500 ? "err" : (l.status >= 400 ? "warn" : "ok")));
      var errCell = cell(l.err || "", "muted trunc");
      if (l.err) errCell.title = l.err;
      tb.appendChild(row([
        cell(fmtTime(l.time), "muted"), cell(l.model, "mono"), cell(l.stream ? "是" : "否", "mono"),
        st, cell(l.latency_ms + "ms", "mono"), cell(l.prompt_len + " / " + l.output_len, "mono"),
        cell(l.account, "mono"), cell(l.key, "mono"), errCell
      ]));
    });
  }

  function renderSettings(s){
    // 用户正在编辑设置时不要用服务端值覆盖 —— 否则在别的页签点一下
    // 「探活」触发的 refresh() 会静默丢掉未保存的改动。
    $("#sPw").placeholder = s.admin_password_set ? "已设置，留空则不修改" : "未设置";
    if (settingsDirty) return;
    var p = s.pool || {};
    $("#sPoolStrategy").value = p.strategy || "least_inflight";
    $("#sPoolConcurrency").value = p.max_in_flight === undefined ? 2 : p.max_in_flight;
    $("#sPoolSticky").value = p.sticky_ttl_sec === undefined ? 1800 : p.sticky_ttl_sec;
    $("#sPoolCooldown").value = p.rate_cooldown_sec === undefined ? 60 : p.rate_cooldown_sec;
    $("#sPoolThreshold").value = p.breaker_threshold === undefined ? 3 : p.breaker_threshold;
    $("#sPoolBreaker").value = p.breaker_cooldown_sec === undefined ? 300 : p.breaker_cooldown_sec;
    $("#sDefaultModel").value = s.default_model || "";
    $("#sForceModel").value = s.force_model || "";
    $("#sTimeout").value = s.request_timeout_sec || "";
    $("#sRetries").value = s.max_retries || "";
    $("#sLogKeep").value = s.log_keep || "";
    $("#sLiveTTL").value = s.live_ttl_sec || "";
    $("#sCors").value = s.cors_origin || "";
    $("#sUserData").value = s.user_data_dir || "";
    $("#sLoginPort").value = s.login_port || 0;
    $("#sAllowCredit").checked = !!s.allow_credit_models;
    $("#sAllowAnonymous").checked = !!s.allow_anonymous;
    $("#sRateLimit").checked = !!s.rate_limit_enabled;
    $("#sRateQPS").value = s.rate_limit_qps || "";
    $("#sRateBurst").value = s.rate_limit_burst || "";
    $("#sRateConc").value = (s.rate_limit_concurrency === undefined ? "" : s.rate_limit_concurrency);
    $("#sAutoClaim").checked = !!s.auto_claim;
    $("#sClaimHour").value = (s.claim_after_hour === undefined ? 0 : s.claim_after_hour);
    $("#sClaimMinute").value = (s.claim_after_minute === undefined ? 5 : s.claim_after_minute);
    $("#sAutoRefresh").checked = !!s.auto_refresh;
    $("#sWriteBack").checked = !!s.write_back_to_ide;
    $("#sPluginName").value = s.plugin_name || "";
    $("#sPluginVersion").value = s.plugin_version || "";
    $("#sIDEName").value = s.ide_name || "";
    $("#sIDEVersion").value = s.ide_version || "";
    $("#sAliases").value = JSON.stringify(s.model_aliases || {}, null, 2);
  }

  function markSettingsDirty(){
    if (settingsDirty) return;
    settingsDirty = true;
    $("#settingsDirty").classList.remove("hidden");
  }
  function clearSettingsDirty(){
    settingsDirty = false;
    $("#settingsDirty").classList.add("hidden");
  }

  function refresh(){
    return api("/admin/api/state").then(function(d){
      state.data = d;
      renderOverview(d);
      if ($("#tab-models").classList.contains("on")) {if(!$("#pricingRows").children.length)loadPricing();if(!$("#modelPolicies").children.length)loadModelPolicies();}
      if ($("#tab-overview").classList.contains("on")) refreshUsageLogs();
      renderAccounts(d);
      renderKeys(d);
      renderSettings(d.settings || {});
      if($("#tab-audit").classList.contains("on"))loadAudit();
      loadHealth();
      var now = new Date();
      var p2 = function(n){ return (n < 10 ? "0" : "") + n; };
      $("#lastSync").textContent = "已更新 " + p2(now.getHours()) + ":" + p2(now.getMinutes()) + ":" + p2(now.getSeconds());
      // 账号页可见时补一次额度（首次进页签时 refresh 可能还没回来）
      if ($("#tab-accounts").classList.contains("on")) loadCredits(false);
    }).catch(function(e){
      if (e.status === 401) {
        if (localPanelSession) {
          toast("本机面板会话已失效，请刷新页面后继续。", "err");
        } else {
          showAdminLogin();
          $("#pw").focus();
        }
      } else if (e.status === 403) {
        toast("管理接口拒绝访问：未设置密码时仅允许本机直连\n" + e.message, "err");
      } else {
        toast("加载失败：" + e.message, "err");
      }
    });
  }
  function refreshLogs(){
    var limit = $("#logLimit").value;
    return api("/admin/api/logs?limit=" + encodeURIComponent(limit)).then(function(d){
      renderLogs(d.logs);
    }).catch(function(e){ toast("日志加载失败：" + e.message, "err"); });
  }

  function accountOp(action, extra){
    var body = { action: action };
    if (extra) for (var k in extra) body[k] = extra[k];
    return api("/admin/api/accounts", {method:"POST", body:body}).then(function(){
      toast("操作成功", "ok");
      return refresh();
    }).catch(function(e){ toast("失败：" + e.message, "err"); });
  }
  function keyOp(kind, key){
    return api("/admin/api/keys?" + kind + "=" + encodeURIComponent(key), {method:"DELETE"})
      .then(function(){ toast("操作成功","ok"); return refresh(); })
      .catch(function(e){ toast("失败：" + e.message, "err"); });
  }
  function checkin(id){
    toast("探活中，请稍候…");
    return api("/admin/api/checkin", {method:"POST", body: id ? {id:id} : {}}).then(function(r){
      var results = r.results || [];
      var bad = results.filter(function(x){ return !x.ok; });
      toast("探活完成：" + results.length + " 个账号，失败 " + bad.length +
        (bad.length ? "\n" + bad.map(function(x){ return x.name + ": " + x.err; }).join("\n") : ""),
        bad.length ? "err" : "ok");
      return refresh();
    }).catch(function(e){ toast("探活失败：" + e.message, "err"); });
  }

  function claim(id){
    toast("领取中，请稍候…");
    return api("/admin/api/claim", {method:"POST", body: id ? {id:id} : {}}).then(function(r){
      var results = r.results || [];
      var bad = results.filter(function(x){ return !x.ok; });
      toast("领取完成：" + (r.succeeded||0) + "/" + (r.total||0) + " 成功" +
        (bad.length ? "\n" + bad.map(function(x){ return x.name + ": " + x.message; }).join("\n") : ""),
        bad.length ? "err" : "ok");
      return refresh();
    }).catch(function(e){ toast("领取失败：" + e.message, "err"); });
  }

  function exportSession(id){
    toast("导出中…");
    api("/admin/api/accounts", {method:"POST", body:{action:"export", id:id}}).then(function(r){
      $("#exportJSON").value = r.session_json || "";
      $("#exportMeta").textContent = "账号 " + (r.name||"") +
        " · 凭据有效期至 " + fmtTime(r.expires_at) +
        " · 可续期 " + (r.has_refresh ? "是" : "否");
      $("#exportPanel").classList.remove("hidden");
      toast("已导出（含 refresh_token，请勿外传）", "ok");
    }).catch(function(e){ toast("导出失败：" + e.message, "err"); });
  }

  // Browser authorization is staged; polling only previews, explicit complete saves.
  var loginSid = null, loginDraft = null, loginPreview = null, loginBusy = false;
  var LOGIN_KEY = "codearts2api_login_flow";
  function stopLogin(){
    loginSid = null; loginDraft = null; loginPreview = null;
    try { sessionStorage.removeItem(LOGIN_KEY); } catch(e){}
  }
  function rememberLogin(r){
    loginDraft = {session_id:r.session_id, auth_url:r.auth_url, callback:r.callback,
      tunnel_hint:r.tunnel_hint, expires_at:Date.now() + (r.expires_in || 300)*1000};
    try { sessionStorage.setItem(LOGIN_KEY, JSON.stringify(loginDraft)); } catch(e){}
  }
  function resumeLogin(){
    var saved;
    try { saved = JSON.parse(sessionStorage.getItem(LOGIN_KEY) || "null"); } catch(e){}
    if (!saved || !saved.session_id || saved.expires_at <= Date.now()) { stopLogin(); return; }
    if (!isTrustedAuthURL(saved.auth_url)) { stopLogin(); return; }
    loginDraft = saved; loginSid = saved.session_id;
  }
  function drawLogin(){
    var active = !!loginDraft;
    $("#loginInitial").classList.toggle("hidden", active);
    $("#loginDetails").classList.toggle("hidden", !active);
    $("#loginCancel").classList.toggle("hidden", !active);
    $("#loginRestart").classList.toggle("hidden", !active);
    $("#loginPreview").classList.toggle("hidden", !loginPreview);
    $("#loginComplete").classList.toggle("hidden", !loginPreview);
    if (active) setLoginURL(loginDraft.auth_url,loginDraft.callback,loginDraft.tunnel_hint);
    var step = loginPreview ? 3 : active ? 2 : 1;
    ["loginStep1","loginStep2","loginStep3"].forEach(function(id,i){ $("#"+id).classList.toggle("active", i+1 === step); });
  }
  function setLoginBusy(busy){
    loginBusy = busy;
    ["loginGenerate","loginSubmit","loginCheck","loginCancel","loginRestart","loginComplete"].forEach(function(id){$("#"+id).disabled=busy;});
  }
  function loginFailure(e){ $("#loginError").textContent=e.message || String(e); $("#loginError").classList.remove("hidden"); }
  function openLogin(){
    $("#loginError").classList.add("hidden");
    drawLogin();
    if (!$("#loginPanel").open) $("#loginPanel").showModal();
    if(loginSid && !loginBusy){setLoginBusy(true);return pollLogin().catch(loginFailure).finally(function(){setLoginBusy(false);});}
  }
  function generateLogin(){
    if (loginBusy) return;
    setLoginBusy(true); $("#loginError").classList.add("hidden");
    return api("/admin/api/login/start",{method:"POST",body:{}}).then(function(r){
      if (!isTrustedAuthURL(r.auth_url)) throw new Error("授权地址无效");
      loginSid=r.session_id;loginPreview=null;rememberLogin(r);drawLogin();
      $("#loginPasted").value="";
      $("#loginStatus").textContent="链接已生成。请复制到另一浏览器完成登录，再返回提交授权信息。";
    }).catch(loginFailure).finally(function(){setLoginBusy(false);});
  }
  function pollLogin(){
    if (!loginSid) return Promise.reject(new Error("请先生成登录链接"));
    return api("/admin/api/login/poll",{method:"POST",body:{session_id:loginSid}}).then(function(r){
      if (r.saved) { stopLogin();drawLogin();$("#loginStatus").textContent="本次登录已保存，可关闭窗口。";return refresh(); }
      if (r.status === "confirmed" && r.ready) {
        $("#loginError").textContent="";$("#loginError").classList.add("hidden");
        loginPreview=r;
        $("#loginPasted").value="";
        $("#loginAccountName").textContent=r.account_name || r.account || "码道账号";
        $("#loginAccountInfo").textContent=(r.domain_id ? "账号标识："+r.domain_id+" · " : "") + (r.has_refresh ? "可自动续期" : "临时凭据，到期需重新登录") + " · 有效期至 "+fmtTime(r.expires_at);
        $("#loginReplaceLabel").classList.toggle("hidden", !r.existing);
        $("#loginReplace").checked=false;
        $("#loginStatus").textContent="授权已验证，尚未保存。请核对账号，再点击「确认完成登录」。";
      } else {
        loginPreview=null;
        $("#loginStatus").textContent=r.message || "尚未收到授权，请在另一浏览器登录后重试。";
        if(["failed","expired","canceled"].indexOf(r.status)>=0) loginFailure(new Error((r.message || "本次授权未完成")+"。请重新生成登录链接。"));
      }
      drawLogin();
    });
  }
  var deletingAccount = null, deleteBusy = false;
  function showDeleteAccount(account){
    deletingAccount=account;
    $("#deleteAccountName").textContent=account.name || account.id;
    $("#deleteAccountError").classList.add("hidden");
    $("#deleteAccountConfirm").disabled=false;
    if (!$("#deleteAccountDialog").open) $("#deleteAccountDialog").showModal();
  }

  var WELFARE_STATUS = {
    ELIGIBLE:  ["可领取", "ok"],
    CLAIMED:   ["已领取", "warn"],
    CONFIRMED: ["已到账", "ok"],
    CONSUMED:  ["已使用", ""],
    ENTRY:     ["需先完成前置", "warn"]
  };

  function renderWelfare(campaigns){
    var tb = $("#welfare");
    tb.textContent = "";
    if (!campaigns || !campaigns.length) { tb.appendChild(emptyRow(7, "暂无活动")); return; }
    campaigns.forEach(function(c){
      var st = WELFARE_STATUS[c.status] || [c.status || "—", ""];
      var stCell = cell(""); stCell.appendChild(pill(st[0], st[1]));

      var autoCell = cell("");
      autoCell.appendChild(c.auto_claimable ? pill("是", "ok") : pill("否", ""));

      var reward = c.benefit_amount ? (c.benefit_amount + " " + (c.benefit_unit || "")) : "—";
      var period = (c.start_time || "").slice(0,10) + " ~ " + (c.end_time || "").slice(0,10);

      tb.appendChild(row([
        cell(c.title), cell(c.type, "mono"), cell(reward, "mono"),
        stCell, autoCell, cell(c.trigger_event || c.trigger_mode || "—", "mono"),
        cell(period, "muted")
      ]));
    });
  }

  // 当前选中的福利账号（空 = 让后端按调度池挑一个）
  var welfareAccountID = "";

  function loadWelfare(){
    $("#welfareHint").textContent = "加载中…";
    var q = welfareAccountID ? ("?id=" + encodeURIComponent(welfareAccountID)) : "";
    return api("/admin/api/welfare" + q).then(function(r){
      renderWelfare(r.campaigns);
      renderWelfareAccountPicker(r);
      $("#welfareHint").textContent = r.claimable_count
        ? ("当前账号 " + (r.account || "") + "，可领 " + r.claimable_count + " 项")
        : ("当前账号 " + (r.account || "") + "，暂无可领");
    }).catch(function(e){
      $("#welfareHint").textContent = "";
      toast("获取活动列表失败：" + e.message, "err");
    });
  }

  // renderWelfareAccountPicker 把账号下拉框填满，并同步当前选择。
  //
  // 之前只显示一个账号名，很容易被误解成「只有这个号能领」——实际上
  // 活动清单是按账号查的，每个号的可领项都不同。
  function renderWelfareAccountPicker(r){
    var sel = $("#welfareAccount");
    if (!sel) return;
    var list = r.accounts || [];
    sel.textContent = "";
    list.forEach(function(a){
      var o = document.createElement("option");
      o.value = a.id;
      o.textContent = a.name + (a.permanent ? "（永久 AK/SK）" : "");
      if (a.id === r.id) o.selected = true;
      sel.appendChild(o);
    });
    if (r.id) welfareAccountID = r.id;
    sel.disabled = list.length <= 1;
    // 账号只有一个时说明白，别让用户以为漏了
    sel.title = list.length <= 1
      ? "当前只有 1 个账号"
      : "活动清单按账号查询，每个账号的可领项不同";
  }

  // isTrustedAuthURL 只允许华为云门户 / 本机回环的授权地址。
  //
  // 授权 URL 由网关自己拼出来（/admin/api/login/start），理论上不可注入；
  // 但这里是唯一一处「拿后端返回的 URL 去跳转」的地方，加一道白名单成本很低，
  // 可以挡住管理密码泄露、或将来某个 bug 让 portal_host 变成可控的情况。
  function isTrustedAuthURL(raw){
    var u;
    try { u = new URL(raw); } catch { return false; }
    if (u.protocol !== "https:" && u.protocol !== "http:") return false;
    var host = u.hostname.toLowerCase();
    if (u.protocol === "http:") {
      // 明文 http 只允许本机（本地联调）
      return host === "127.0.0.1" || host === "localhost" || host === "::1";
    }
    return host === "huaweicloud.com" || host.endsWith(".huaweicloud.com")
        || host === "huaweicloud.cn"  || host.endsWith(".huaweicloud.cn")
        || host === "huawei.com"      || host.endsWith(".huawei.com");
  }

  // setLoginURL 把授权地址写进只读输入框，并只在通过白名单时才挂到链接上。
  function setLoginURL(raw, callback, tunnel){
    var ok = isTrustedAuthURL(raw);
    $("#loginURL").value = raw || "";
    $("#loginCallback").textContent = callback || "—";
    $("#loginTunnel").value = tunnel || "";
    var a = $("#loginLink");
    if (ok) {
      a.href = raw;
      a.classList.remove("hidden");
    } else {
      a.removeAttribute("href");
      a.classList.add("hidden");
      toast("授权地址不在白名单内，已禁止直接打开：\n" + raw +
        "\n（仅允许 *.huaweicloud.com / *.huaweicloud.cn / *.huawei.com 与本机回环）", "err");
    }
    return ok;
  }

  var providerSession="",providerPreviewID="",providerBusy=false;
  function providerError(e){$("#providerLoginError").textContent=e.message || String(e);}
  function resetProviderLogin(){providerSession="";providerPreviewID="";$("#providerLoginURL").value="";$("#providerLoginCallback").value="";$("#providerPreview").classList.add("hidden");$("#providerLoginOpen").classList.add("hidden");$("#providerLoginOpen").removeAttribute("href");$("#providerLoginCopy").disabled=true;$("#providerLoginCheck").disabled=true;$("#providerScope").disabled=false;$("#providerLoginStatus").textContent="";$("#providerLoginError").textContent="";$("#providerReplace").checked=false;}
  function providerJob(fn){if(providerBusy)return Promise.resolve();providerBusy=true;$("#providerLoginError").textContent="";return Promise.resolve().then(fn).catch(providerError).finally(function(){providerBusy=false;});}
  function providerCall(action,extra){return api("/admin/api/provider-login",{method:"POST",body:Object.assign({action:action,session_id:providerSession},extra || {})});}
  function refreshAfterProviderSave(){resetProviderLogin();$("#providerImportJSON").value="";$("#providerLoginDialog").close();toast("WorkBuddy 账号已保存，正在刷新模型目录","ok");return api("/admin/api/models/refresh",{method:"POST"}).catch(function(e){toast("账号已保存，模型目录刷新失败："+e.message,"err");}).then(refresh);}
  function closeProviderLogin(){if(providerBusy)return;return providerJob(function(){var cancel=providerSession?providerCall("cancel"):Promise.resolve();return cancel.then(function(){resetProviderLogin();$("#providerImportJSON").value="";$("#providerLoginDialog").close();});});}
  function trustedWorkBuddyURL(raw){try{var u=new URL(raw);return u.protocol==="https:"&&!u.username&&!u.password&&["workbuddy.cn","workbuddy.ai","codebuddy.cn","codebuddy.ai","tencent.com","qq.com"].some(function(h){return u.hostname===h||u.hostname.endsWith("."+h);});}catch(e){return false;}}
  // --- events ---
  var TAB_IDS = { accounts:1, overview:1, models:1, keys:1, logs:1, audit:1, settings:1 };
  var TAB_META = {
    overview: { title:"用量", hint:"请求、Token 与耗时统计", doc:"用量" },
    models: { title:"模型", hint:"模型目录、积分倍率与可用性", doc:"模型" },
    accounts: { title:"账号池", hint:"账号状态、额度与调度", doc:"账号池" },
    keys:     { title:"API 密钥", hint:"客户端接入与密钥管理", doc:"API 密钥" },
    logs:     { title:"运行日志", hint:"请求记录与错误排查", doc:"运行日志" },
    audit: { title:"管理审计", hint:"管理操作、登录与敏感信息访问记录", doc:"管理审计" },
    settings: { title:"配置", hint:"调度策略与运行参数", doc:"配置" }
  };
  // activateTab 是唯一的页签切换入口：同步 class / aria-selected / URL hash。
  // warm=false 时只切显示、不触发额外请求（首次加载用）。
  function activateTab(name, updateHash, warm){
    if (!TAB_IDS[name]) return;
    if (name !== "keys" && state.data) renderKeys(state.data);
    document.querySelectorAll("nav button").forEach(function(x){
      var sel = x.dataset.tab === name;
      x.classList.toggle("on", sel);
      x.setAttribute("aria-selected", sel ? "true" : "false");
      x.tabIndex = sel ? 0 : -1;
    });
    document.querySelectorAll("section[role=tabpanel]").forEach(function(x){
      x.classList.toggle("on", x.id === "tab-" + name);
    });
    var meta = TAB_META[name];
    $("#pageTitle").textContent = meta.title;
    $("#pageHint").textContent = meta.hint;
    document.title = meta.doc + " · CodeArts2API 控制台";
    if (updateHash) {
      try { history.replaceState(null, "", "#" + name); } catch { location.hash = name; }
    }
    if (warm === false || (!adminAuthenticated && !localPanelSession)) return;
    if (name === "audit") loadAudit();
    if (name === "logs") refreshLogs();
    if (name === "overview") refreshUsageLogs();
    if (name === "models") {loadPricing();loadModelPolicies();}
    if (name === "accounts") loadCredits(false);
  }
  document.querySelectorAll("nav button").forEach(function(b){
    b.onclick = function(){ activateTab(b.dataset.tab, true); };
    // WAI-ARIA tabs：左右方向键切换
    b.addEventListener("keydown", function(e){
      var keys = Object.keys(TAB_IDS);
      var i = keys.indexOf(b.dataset.tab);
      if (e.key === "ArrowRight") { i = (i + 1) % keys.length; }
      else if (e.key === "ArrowLeft") { i = (i - 1 + keys.length) % keys.length; }
      else return;
      e.preventDefault();
      activateTab(keys[i], true);
      document.querySelector('nav button[data-tab="' + keys[i] + '"]').focus();
    });
  });
  window.addEventListener("hashchange", function(){
    activateTab(location.hash.slice(1), false);
  });

  on("#workbuddyLogin",function(){if(!adminAuthenticated&&!localPanelSession){showAdminLogin();return;}$("#providerLoginDialog").showModal();});
  on("#providerLoginClose",closeProviderLogin);
  $("#providerLoginDialog").addEventListener("cancel",function(e){e.preventDefault();closeProviderLogin();});
  on("#providerLoginStart",function(){return providerJob(function(){var previous=providerSession?providerCall("cancel"):Promise.resolve();return previous.then(function(){resetProviderLogin();return providerCall("start",{provider:"workbuddy",scope:$("#providerScope").value});}).then(function(d){if(!trustedWorkBuddyURL(d.url))throw new Error("授权链接不在 WorkBuddy 官方域名范围");providerSession=d.session_id;$("#providerLoginURL").value=d.url;$("#providerLoginOpen").href=d.url;$("#providerLoginOpen").classList.remove("hidden");$("#providerScope").disabled=true;$("#providerLoginCopy").disabled=false;$("#providerLoginCheck").disabled=false;$("#providerLoginStatus").textContent="链接已生成。请复制到另一浏览器完成授权，再点击检查。";});});});
  on("#providerLoginCopy",function(){return copyText($("#providerLoginURL").value,"WorkBuddy 授权链接已复制");});
  on("#providerLoginCheck",function(){return providerJob(function(){return providerCall("check",{input:$("#providerLoginCallback").value.trim()}).then(function(d){if(!d.ready){$("#providerLoginStatus").textContent=d.message || "尚未完成授权";return;}providerPreviewID=d.preview_id;$("#providerPreviewText").textContent=(d.name || "WorkBuddy 账号")+" · "+d.uid+" · "+(d.scope==="global"?"国际版":"国内版")+(d.existing?" · 已存在，保存将更新凭据":" · 新账号");$("#providerPreview").classList.remove("hidden");$("#providerLoginStatus").textContent="账号已验证，点击确认保存才会加入账号池。";$("#providerLoginCallback").value="";});});});
  on("#providerLoginComplete",function(){return providerJob(function(){return providerCall("complete",{preview_id:providerPreviewID,replace_existing:$("#providerReplace").checked,name:$("#providerAccountName").value.trim()}).then(refreshAfterProviderSave);});});
  on("#providerImport",function(){return providerJob(function(){var credentials;try{credentials=JSON.parse($("#providerImportJSON").value);}catch(e){throw new Error("请粘贴有效的 WorkBuddy JSON 凭据");}return api("/admin/api/provider-import",{method:"POST",body:{provider:"workbuddy",credentials:credentials,replace_existing:$("#providerImportReplace").checked,name:$("#providerAccountName").value.trim()}}).then(refreshAfterProviderSave);});});
  $("#accountProvider").addEventListener("change",function(){if(state.data)renderAccounts(state.data);});
  $("#usageProvider").addEventListener("change",function(){usageView.page=1;refreshUsageLogs();});
  on("#savePw", function(){
    return adminLogin(pw()).then(function(){ resumeLogin(); return refresh(); }).catch(function(e){$("#adminLoginError").textContent=e.message;});
  });
  on("#forgetPw", function(){
    return api("/admin/auth/logout",{method:"POST"}).then(function(){clearStoredPw();location.reload();});
  });
  // 顶栏密码框折叠：默认只露一个锁图标，点开才展开输入框。
  on("#themeToggle", function(){
    var theme = document.documentElement.dataset.theme === "light" ? "dark" : "light";
    document.documentElement.dataset.theme = theme;
    try { localStorage.setItem("codearts2api_theme", theme); } catch(e){}
    updateThemeLabel();
  });
  on("#copyBaseURL", function(){ return copyText(location.origin + "/v1", "API 地址已复制"); });
  $("#apiBaseURL").textContent = location.origin + "/v1";
  $("#accountSearch").addEventListener("input", function(){ if (state.data) renderAccounts(state.data); });
  document.querySelectorAll("[data-account-filter]").forEach(function(button){
    button.addEventListener("click", function(){
      accountFilter = button.dataset.accountFilter;
      document.querySelectorAll("[data-account-filter]").forEach(function(b){
        b.classList.toggle("on", b === button);
        b.setAttribute("aria-pressed", String(b === button));
      });
      if (state.data) renderAccounts(state.data);
    });
  });
  on("#pwToggle", function(){if(!adminAuthenticated)showAdminLogin();});
  $("#adminLoginDialog").addEventListener("cancel",function(e){e.preventDefault();});
  function updatePwToggle(){
    $("#localPanelStatus").classList.toggle("hidden", !localPanelSession);
    $("#pwToggleText").textContent=adminAuthenticated?"管理员已登录":"管理员登录";
    $("#forgetPw").classList.toggle("hidden", !adminAuthenticated);
  }
  on("#refresh", function(){
    var jobs = [refresh()];
    if ($("#tab-logs").classList.contains("on")) jobs.push(refreshLogs());
    return Promise.all(jobs);
  });
  on("#reloadModelPolicies",loadModelPolicies);on("#policySave",saveModelPolicy);on("#policyCancel",closeModelPolicy);on("#policyClose",closeModelPolicy);
  $("#policyDialog").addEventListener("cancel",function(e){if(savingPolicy)e.preventDefault();else editingPolicy=null;});
  on("#reloadPricing", loadPricing);
  on("#reloadUsage", refreshUsageLogs);
  on("#applyUsageDates", applyUsageDates);
  on("#usageJump", jumpUsagePage);
  $("#usagePageJump").addEventListener("keydown",function(e){if(e.key==="Enter")jumpUsagePage();});
  $("#usagePreset").addEventListener("change",setUsagePreset);
  ["#usageStart","#usageEnd"].forEach(function(id){$(id).addEventListener("change",function(){$("#usagePreset").value="custom";});});
  $("#usagePageSize").addEventListener("change",function(){usageView.pageSize=Number(this.value);usageView.page=1;try{localStorage.setItem("codearts-usage-page-size",this.value);}catch{}refreshUsageLogs();});
  try{var savedSize=Number(localStorage.getItem("codearts-usage-page-size"));if([20,50,100].indexOf(savedSize)>=0)usageView.pageSize=savedSize;}catch{}
  $("#usageTimezone").textContent="日期按浏览器时区："+Intl.DateTimeFormat().resolvedOptions().timeZone;
  on("#priceEditSave",savePriceEditor);on("#priceEditCancel",closePriceEditor);on("#priceEditClose",closePriceEditor);
  $("#priceEditDialog").addEventListener("cancel",function(e){if(priceSaving)e.preventDefault();else priceEditing=null;});
  on("#importLocal", function(){ return accountOp("import", {}); });
  // 凭据类型切换：永久 AK/SK 不需要 security_token，隐藏该项并更新提示。
  // 注意 select 用 change 事件，不能复用只绑 onclick 的 on()。
  $("#manualKind").addEventListener("change", function(){
    var isAKSK = this.value === "aksk";
    $("#manualSTSField").classList.toggle("hidden", isAKSK);
    if (isAKSK) $("#manualSTS").value = "";
    $("#manualKindHint").textContent = isAKSK
      ? "永久 AK/SK 不过期、无需续期；请勿与 STS 临时凭据混用。"
      : "STS 临时凭据约 2 小时过期，到期需重新录入或用 refresh_token 续期。";
  });
  on("#addManual", function(){
    var ak = $("#manualAK").value.trim(), sk = $("#manualSK").value.trim();
    var kind = $("#manualKind").value;
    if (!ak || !sk) { toast("请填写 Access Key 与 Secret Key", "err"); return; }
    var payload = {
      name: $("#manualName").value.trim(), access_key: ak, secret_key: sk,
      credential_kind: kind
    };
    if (kind !== "aksk") payload.security_token = $("#manualSTS").value.trim();
    return accountOp("manual", payload).then(function(){
      $("#manualAK").value = ""; $("#manualSK").value = ""; $("#manualSTS").value = "";
    });
  });
  on("#addSession", function(){
    var js = $("#sessionJSON").value.trim();
    if (!js) { toast("请粘贴 session JSON", "err"); return; }
    return accountOp("session-json", {name: $("#sessionName").value.trim(), session_json: js}).then(function(){
      $("#sessionJSON").value = "";
    });
  });
  on("#checkinAll", function(){ return checkin(null); });
  on("#refreshCredits", function(){ return loadCredits(true); });
  on("#probeAllModels", function(){
    var ids = Object.keys(lastModels).filter(function(id){
      return lastModels[id].source !== "alias";
    });
    if (!ids.length) { toast("模型清单为空，先点「从上游刷新清单」", "err"); return; }
    // 每个模型都是一次真实请求、真实扣积分，全量探测前必须二次确认。
    if (!window.confirm("将向 " + ids.length + " 个模型各发一次真实请求（max_tokens=8，会真实扣积分），确定继续？")) return;
    ids.forEach(function(id){ modelProbeState[id] = { state: "busy" }; redrawModelRow(id); });
    $("#modelHint").textContent = "正在测活 " + ids.length + " 个模型…（每个会真实消耗极少量积分）";

    return api("/admin/api/models/probe", {method:"POST", body:{}}).then(function(r){
      (r.results || []).forEach(function(x){
        modelProbeState[x.model] = x.ok
          ? { state: "ok", account: x.account }
          : { state: "err", err: x.error || "未知错误", account: x.account };
        redrawModelRow(x.model);
      });
      $("#modelHint").textContent = "可用 " + (r.available||0) + " / " + (r.total||0) +
        (r.unavailable ? "，不可用 " + r.unavailable : "");
      toast("测活完成：" + (r.available||0) + "/" + (r.total||0) + " 可用", "ok");
    }).catch(function(e){
      var msg = (e && e.message) ? e.message : "未知错误";
      ids.forEach(function(id){ modelProbeState[id] = { state: "err", err: msg }; redrawModelRow(id); });
      $("#modelHint").textContent = "测活失败：" + msg;
      toast("测活失败：" + msg, "err");
    });
  });
  on("#claimAll", function(){ return claim(null); });
  on("#loadWelfare", loadWelfare);
  // 切换账号后重新拉该账号的活动清单（各账号的领取状态不同）
  $("#welfareAccount").addEventListener("change", function(){
    welfareAccountID = this.value;
    loadWelfare();
  });
  on("#claimWelfareNow", function(){ return claim(null).then(loadWelfare); });
  on("#genInvite", function(){
    toast("生成中…");
    // 用当前选中的账号生成，避免「看到的是 A 号、生成的是 B 号」
    var body = welfareAccountID ? {id: welfareAccountID} : {};
    return api("/admin/api/invite", {method:"POST", body:body}).then(function(r){
      $("#inviteURL").value = r.invite_url || "";
      $("#inviteStat").textContent = "已激活 " + (r.activation_count||0) + " / " + (r.max_activations||0) +
        " 次 · 状态 " + (r.status||"") + " · 账号 " + (r.account||"");
      $("#invitePanel").classList.remove("hidden");
      toast("已生成邀请链接", "ok");
    }).catch(function(e){ toast("生成失败：" + e.message, "err"); });
  });
  on("#copyInvite", function(){ return copyText($("#inviteURL").value, "已复制邀请链接"); });
  on("#browserLogin", function(){
    if (!localPanelSession && !adminAuthenticated) { showAdminLogin();return; }
    openLogin();
  });
  on("#loginGenerate",generateLogin);
  on("#loginClose",function(){ $("#loginPasted").value="";$("#loginPanel").close(); });
  $("#loginPanel").addEventListener("cancel",function(){ $("#loginPasted").value=""; });
  on("#loginCopy",function(){return copyText($("#loginURL").value,"链接已复制，请在另一个浏览器打开");});
  on("#loginCheck",function(){
    if (loginBusy) return;
    setLoginBusy(true);$("#loginError").classList.add("hidden");
    return pollLogin().catch(loginFailure).finally(function(){setLoginBusy(false);});
  });
  on("#loginComplete",function(){
    if (loginBusy || !loginPreview || !loginSid) return;
    if (loginPreview.existing && !$("#loginReplace").checked) {loginFailure(new Error("请勾选确认更新已有账号，或重新生成链接登录其他账号"));return;}
    setLoginBusy(true);$("#loginError").classList.add("hidden");
    return api("/admin/api/login/complete",{method:"POST",body:{session_id:loginSid,preview_id:loginPreview.preview_id,replace_existing:$("#loginReplace").checked}}).then(function(){
      stopLogin();drawLogin();$("#loginPanel").close();toast("账号已保存", "ok");return refresh();
    }).catch(loginFailure).finally(function(){setLoginBusy(false);});
  });
  on("#deleteAccountCancel",function(){if(!deleteBusy){$("#deleteAccountDialog").close();deletingAccount=null;}});
  $("#deleteAccountDialog").addEventListener("cancel",function(e){if(deleteBusy)e.preventDefault();else deletingAccount=null;});
  on("#deleteAccountConfirm",function(){
    if (!deletingAccount || deleteBusy) return;
    var id=deletingAccount.id;
    deleteBusy=true;$("#deleteAccountCancel").disabled=true;$("#deleteAccountError").classList.add("hidden");
    return api("/admin/api/accounts",{method:"POST",body:{action:"remove",id:id}}).then(function(r){
      if(!r.ok || r.removed_id!==id) throw new Error("删除未完成，请刷新后重试");
      delete creditState[id];deletingAccount=null;$("#deleteAccountDialog").close();
      toast("账号已删除", "ok");return refresh();
    }).catch(function(e){$("#deleteAccountError").textContent=e.message;$("#deleteAccountError").classList.remove("hidden");})
      .finally(function(){deleteBusy=false;$("#deleteAccountCancel").disabled=false;});
  });
  on("#copyExport", function(){
    return copyText($("#exportJSON").value, "已复制到剪贴板（注意不要外传）");
  });
  on("#closeExport", function(){
    $("#exportJSON").value = "";
    $("#exportPanel").classList.add("hidden");
  });
  on("#loginSubmit",function(){
    if (loginBusy) return;
    var input=$("#loginPasted").value.trim();
    if(!loginSid){loginFailure(new Error("请先生成登录链接"));return;}
    if(!input){$("#loginCheck").click();return;}
    setLoginBusy(true);loginPreview=null;drawLogin();$("#loginError").classList.add("hidden");
    return api("/admin/api/login/callback",{method:"POST",body:{session_id:loginSid,input:input}}).then(function(){
      $("#loginPasted").value="";$("#loginStatus").textContent="授权信息已提交，正在换取凭据。稍后点击检查授权信息。";return pollLogin();
    }).catch(loginFailure).finally(function(){setLoginBusy(false);});
  });
  on("#copyTunnel",function(){return copyText($("#loginTunnel").value,"转发命令已复制");});
  function cancelLogin(restart){
    if(loginBusy)return;
    setLoginBusy(true);$("#loginError").classList.add("hidden");
    return api("/admin/api/login/cancel",{method:"POST",body:{session_id:loginSid}}).then(function(){
      stopLogin();drawLogin();$("#loginPasted").value="";$("#loginStatus").textContent="先生成链接，再到另一浏览器完成授权。";
      setLoginBusy(false);if(restart)return generateLogin();
    }).catch(loginFailure).finally(function(){setLoginBusy(false);});
  }
  on("#loginCancel",function(){return cancelLogin(false);});
  on("#loginRestart",function(){return cancelLogin(true);});
  on("#refreshModels", function(){
    toast("正在从上游刷新模型清单…");
    api("/admin/api/models/refresh", {method:"POST"}).then(function(r){
      toast("已刷新，共 " + (r.models||[]).length + " 个模型"+(Object.keys(r.provider_errors || {}).length?"；部分上游刷新失败，请查看状态":""), Object.keys(r.provider_errors || {}).length?"err":"ok");
      return refresh();
    }).catch(function(e){ toast("刷新失败：" + e.message, "err"); });
  });
  on("#addKey", function(){
    return api("/admin/api/keys", {method:"POST", body:{name:$("#keyName").value.trim()}}).then(function(r){
      toast("密钥已创建，可在列表中随时显示或复制", "ok");
      $("#keyName").value = "";
      return refresh();
    }).catch(function(e){ toast("创建失败：" + e.message, "err"); });
  });
  on("#reloadLogs", refreshLogs);
  on("#clearLogs", function(){
    if (!confirm("确定清空全部日志？")) return;
    api("/admin/api/logs", {method:"DELETE"}).then(function(){ toast("已清空","ok"); return refreshLogs(); })
      .catch(function(e){ toast("失败：" + e.message, "err"); });
  });
  on("#clearStats", function(){
    if (!confirm("确定重置统计？")) return;
    api("/admin/api/stats", {method:"DELETE"}).then(function(){ toast("已重置","ok"); return refresh(); })
      .catch(function(e){ toast("失败：" + e.message, "err"); });
  });
  on("#saveSettings", function(){
    var aliases;
    try { aliases = JSON.parse($("#sAliases").value || "{}"); }
    catch { toast("模型别名不是合法 JSON", "err"); return; }
    var body = {
      pool: {
        strategy: $("#sPoolStrategy").value,
        max_in_flight: Number($("#sPoolConcurrency").value),
        sticky_ttl_sec: Number($("#sPoolSticky").value),
        rate_cooldown_sec: Number($("#sPoolCooldown").value),
        breaker_threshold: Number($("#sPoolThreshold").value),
        breaker_cooldown_sec: Number($("#sPoolBreaker").value)
      },
      model_aliases: aliases,
      default_model: $("#sDefaultModel").value.trim(),
      force_model: $("#sForceModel").value.trim(),
      request_timeout_sec: parseInt($("#sTimeout").value, 10) || 0,
      max_retries: parseInt($("#sRetries").value, 10) || 0,
      log_keep: parseInt($("#sLogKeep").value, 10) || 0,
      live_ttl_sec: parseInt($("#sLiveTTL").value, 10) || 0,
      cors_origin: $("#sCors").value.trim(),
      user_data_dir: $("#sUserData").value.trim(),
      login_port: parseInt($("#sLoginPort").value, 10) || 0,
      allow_credit_models: $("#sAllowCredit").checked,
      allow_anonymous: $("#sAllowAnonymous").checked,
      rate_limit_enabled: $("#sRateLimit").checked,
      rate_limit_qps: parseInt($("#sRateQPS").value, 10) || 0,
      rate_limit_burst: parseInt($("#sRateBurst").value, 10) || 0,
      rate_limit_concurrency: parseInt($("#sRateConc").value, 10) || 0,
      auto_claim: $("#sAutoClaim").checked,
      claim_after_hour: parseInt($("#sClaimHour").value, 10) || 0,
      claim_after_minute: parseInt($("#sClaimMinute").value, 10) || 0,
      auto_refresh: $("#sAutoRefresh").checked,
      write_back_to_ide: $("#sWriteBack").checked,
      plugin_name: $("#sPluginName").value.trim(),
      plugin_version: $("#sPluginVersion").value.trim(),
      ide_name: $("#sIDEName").value.trim(),
      ide_version: $("#sIDEVersion").value.trim()
    };
    var np = $("#sPw").value;
    if (np !== "") body.admin_password = np;
    return api("/admin/api/settings", {method:"POST", body:body}).then(function(r){
      $("#sPw").value = "";
      toast("设置已保存", "ok");
      clearSettingsDirty();
      renderSettings(r.settings || {});
      if (np !== "") return adminLogin(np.trim()).then(refresh);
      return refresh();
    }).catch(function(e){ toast("保存失败：" + e.message, "err"); });
  });
  on("#clearPw", function(){
    if (!confirm("确定清除管理密码？\n清除后管理接口仅允许本机直连访问，反向代理 / 容器场景将无法再打开控制台。")) return;
    return api("/admin/api/settings", {method:"POST", body:{clear_admin_password:true}}).then(function(r){
      $("#sPw").value = "";
      clearStoredPw();
      $("#pw").value = "";
      toast("管理密码已清除", "ok");
      clearSettingsDirty();
      renderSettings(r.settings || {});
      return refresh();
    }).catch(function(e){ toast("清除失败：" + e.message, "err"); });
  });
  $("#logLimit").onchange = refreshLogs;

  // 只读输入框：点击即全选。
  // 用事件绑定而不是内联 onclick 属性，这样 CSP 不需要放开内联事件处理器。
  document.querySelectorAll("[data-select-on-click]").forEach(function(el){
    el.addEventListener("click", function(){ this.select(); });
  });

  // 设置表单一旦被编辑就标记为脏，refresh() 不再覆盖它。
  ["input", "change"].forEach(function(evt){
    $("#tab-settings").addEventListener(evt, markSettingsDirty);
  });

  var auditPage=1,auditTotal=0;
  function loadAudit(){
    if(!adminAuthenticated && !localPanelSession)return Promise.resolve();
    return api("/admin/api/audit?page="+auditPage+"&page_size=50").then(function(d){
      auditTotal=d.total || 0;var tb=$("#auditRows");tb.textContent="";
      (d.events || []).forEach(function(e){tb.appendChild(row([cell(fmtTime(e.time)),cell(e.actor),cell(e.operation),cell(e.path,"mono"),cell(e.peer,"mono"),cell(e.status,"mono")]));});
      if(!tb.children.length)tb.appendChild(emptyRow(6,"暂无管理记录"));
      $("#auditPage").textContent="第 "+auditPage+" 页 · 共 "+auditTotal+" 条";
      $("#auditPrev").disabled=auditPage<=1;$("#auditNext").disabled=auditPage*50>=auditTotal;
    }).catch(function(e){toast(e.message,"err");});
  }
  on("#reloadAudit",loadAudit);on("#auditPrev",function(){auditPage=Math.max(1,auditPage-1);return loadAudit();});on("#auditNext",function(){auditPage++;return loadAudit();});
  var budgetKeyRef="",budgetBusy=false;
  function openKeyBudget(k){
    var b=k.budget || {},u=k.budget_usage || {};budgetKeyRef=k.ref;
    $("#budgetRequests").value=b.max_requests || 0;$("#budgetTokens").value=b.max_tokens || 0;$("#budgetUSD").value=b.max_usd || 0;
    $("#keyBudgetUsage").textContent="已用："+(u.requests || 0)+" 次 / "+(u.tokens || 0)+" Token / $"+Number(u.usd || 0).toFixed(6)+(u.incomplete?" · 存在未报告用量，需核查后重置预算":"")+(u.unpriced?" · 存在未定价消耗，USD 预算需核查后重置":"");
    $("#budgetReset").checked=false;$("#keyBudgetError").textContent="";$("#keyBudgetDialog").showModal();
  }
  on("#keyBudgetClose",function(){if(!budgetBusy)$("#keyBudgetDialog").close();});
  $("#keyBudgetDialog").addEventListener("cancel",function(e){if(budgetBusy)e.preventDefault();});
  on("#keyBudgetSave",function(){
    if(budgetBusy)return;var budget={},fields={max_requests:"#budgetRequests",max_tokens:"#budgetTokens",max_usd:"#budgetUSD"};
    for(var name in fields){var el=$(fields[name]);if(!el.checkValidity() || !el.value.trim()){$("#keyBudgetError").textContent="请填写非负预算";return;}budget[name]=Number(el.value);}
    budgetBusy=true;
    return api("/admin/api/keys/budget",{method:"POST",body:{ref:budgetKeyRef,budget:budget,reset:$("#budgetReset").checked}}).then(function(){$("#keyBudgetDialog").close();toast("预算已保存","ok");return refresh();}).catch(function(e){$("#keyBudgetError").textContent=e.message;}).finally(function(){budgetBusy=false;});
  });
  function openModelHealth(a){
    var tb=$("#modelHealthRows");tb.textContent="";
    Object.keys(a.model_health || {}).sort().forEach(function(model){var v=a.model_health[model];var remaining=Math.max(0,v.cooldown_until-Math.floor(Date.now()/1000));tb.appendChild(row([cell(model,"mono"),cell(v.failures),cell(remaining?"冷却 "+remaining+" 秒":"可调度"),cell(fmtTime(v.updated_at)),cell(v.last_error || "—","muted")]));});
    if(!tb.children.length)tb.appendChild(emptyRow(5,"暂无模型请求记录"));
    $("#modelHealthDialog").showModal();
  }
  on("#modelHealthClose",function(){$("#modelHealthDialog").close();});
  clearStoredPw();
  updatePwToggle();
  $("#pw").addEventListener("keydown", function(e){ if (e.key === "Enter") { $("#savePw").click(); } });
  $("#toast").addEventListener("click", function(){ this.className = "toast"; });

  var initialTab = location.hash.slice(1);
  initialTab = TAB_IDS[initialTab] ? initialTab : "accounts";
  updateThemeLabel();
  activateTab(initialTab, false, true);
  api("/admin/auth/session").then(function(session){
    adminAuthenticated = !!session.authenticated || !!localPanelSession;
    updatePwToggle();
    $("#clearPw").classList.toggle("hidden", !session.local_compatibility);
    if(adminAuthenticated){resumeLogin();return refresh();}
    showAdminLogin();
  }).catch(function(e){showAdminLogin();$("#adminLoginError").textContent=e.message;});
  setInterval(function(){if(adminAuthenticated || localPanelSession)loadHealth();}, 20000);
})();
