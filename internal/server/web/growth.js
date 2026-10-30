// Bounded account workers share the existing account-scoped read/claim APIs.
// Closing the dialog stops queued work; no timers or background queue remain.
(function(){
  "use strict";
  window.CodeArtsGrowth=function(ui){
    var $=function(id){return document.getElementById(id);};
    var dialog=$("growthDialog"),sequence=0,accounts=[],entries=[],busy=false,busyKind="",notice="",controller=null;
    var select=$("growthAccountSelect");
    function active(seq){return sequence===seq&&dialog.open;}
    function claimable(entry,task){return entry.account.enabled&&entry.loaded&&task.claimable&&!entry.blocked.has(task.task_code);}
    function block(entry,code,kind){(kind==="accept"?entry.acceptBlocked:entry.blocked).add(code);}
    function eligible(entry,task,kind){
      if(!entry.account.enabled||!entry.loaded||(kind==="accept"?entry.acceptBlocked:entry.blocked).has(task.task_code))return false;
      if(kind==="claim")return task.claimable;
      if(task.claimed||task.locked)return false;
      if(kind==="accept")return !task.accept_not_required&&task.accept_status==="not_accepted";
      if(task.task_code==="first_buddy"&&entry.buddy&&entry.buddy.owned)return false;
      if(task.task_code!=="first_buddy"&&entry.blocked.has("first_buddy"))return false;
      return task.auto_supported&&(!task.execution_pending||task.task_code==="first_buddy");
    }
    function needsBuddy(entry){return entry.tasks.some(function(t){return t.task_code==="first_buddy"&&t.auto_supported&&!t.claimed;})&&!(entry.buddy&&entry.buddy.owned);}
    function needsConsent(entry){return needsBuddy(entry)&&!(entry.buddy&&entry.buddy.agreed);}
    function runOrder(code,entry){var task=entry.tasks.find(function(t){return t.task_code===code;});return code==="first_buddy"?-2:(task&&task.activity_only?-1:(code==="Model_chat_GLM5.2"?0:1));}
    function runTask(entry,code){
      var codes=[code];
      if(code!=="first_buddy"&&needsBuddy(entry))codes.unshift("first_buddy");
      return operate([{entry:entry,codes:codes}],"run");
    }
    function selectedJobs(kind){
      return entries.map(function(e){return {entry:e,codes:e.tasks.filter(function(t){return eligible(e,t,kind);}).map(function(t){return t.task_code;}).sort(function(a,b){return runOrder(a,e)-runOrder(b,e);})};}).filter(function(job){return job.codes.length;});
    }
    function accepted(entry,code){
      return entry.tasks.some(function(t){return t.task_code===code&&(t.claimed||t.accept_status==="accepted"||t.accept_status==="completed"||t.accept_status==="claimed");});
    }
    async function workers(items,seq,run){
      var next=0;
      async function work(){
        while(active(seq)&&next<items.length){var item=items[next++];await run(item);}
      }
      await Promise.all(Array.from({length:Math.min(2,items.length)},work));
    }
    function render(){
      $("growthRefresh").disabled=busy;
      select.disabled=busy;
      $("growthClaimAll").disabled=busy||!entries.some(function(e){return e.tasks.some(function(t){return claimable(e,t);});});
      $("growthAcceptAll").disabled=busy||!selectedJobs("accept").length;
      var runCount=selectedJobs("run").reduce(function(n,job){return n+job.codes.length;},0);
      $("growthRunAll").disabled=busy||!runCount;
      $("growthBuddyConsent").classList.toggle("hidden",!entries.some(needsConsent));
      $("growthBuddyAgree").disabled=busy;
      $("growthRefresh").textContent=busyKind==="refresh"?"刷新中…":"刷新进度";
      $("growthAcceptAll").textContent=busyKind==="accept"?"接取中…":"接取未报名任务";
      var hasActivity=entries.some(function(e){return e.tasks.some(function(t){return t.activity_only&&eligible(e,t,"run");});});
      $("growthRunAll").textContent=busyKind==="run"?"执行中…":(entries.some(needsBuddy)||hasActivity?"执行成长任务":"执行对话任务")+(runCount?"（"+runCount+"）":"");
      $("growthClaimAll").textContent=busyKind==="claim"?"领取中…":"领取可领奖励";
      $("growthRunAll").title=runCount?"按依赖顺序执行；先执行不调用模型的事件任务，再执行会消耗积分的模型、专家和技能任务":"当前没有可执行的任务；其他任务需在官方应用完成";
      $("growthClaimAll").title=$("growthClaimAll").disabled&&!busy?"当前没有可领取的奖励，请先完成任务并刷新进度":"只领取上游已确认达标的奖励";
      var all=entries.flatMap(function(e){return e.tasks;}),failed=entries.filter(function(e){return e.error;});
      $("growthSummary").textContent=(entries.length>1?entries.length+" 个账号 · ":"")+all.length+" 项任务 · "+all.filter(function(t){return t.claimed;}).length+" 项已领取 · "+all.filter(function(t){return t.claimable;}).length+" 项可领取 · "+all.filter(function(t){return t.auto_supported&&!t.claimed;}).length+" 项支持自动执行";
      $("growthNotice").textContent=notice;
      $("growthError").textContent=failed.length?(failed.length>1?failed.length+" 个账号操作未完成；":"")+(entries.length>1?(failed[0].account.name||"WorkBuddy")+"：":"")+failed[0].error:"";
      var list=$("growthTaskList");list.setAttribute("aria-busy",busy?"true":"false");list.textContent="";
      if(!entries.length){list.appendChild(ui.mk("p","empty","没有支持成长任务的账号"));return;}
      entries.forEach(function(entry){
        var group=ui.mk("div","growth-account");group.dataset.accountId=entry.account.id;
        if(entries.length>1){
          var header=ui.mk("div","growth-account-head");header.appendChild(ui.mk("strong","",entry.account.name||"WorkBuddy"));
          if(!entry.account.enabled)header.appendChild(ui.pill("已停用",""));
          group.appendChild(header);
          if(entry.error)group.appendChild(ui.mk("p","err-text growth-account-error",entry.error));
        }
        if(!entry.loaded){group.appendChild(ui.mk("p","hint",entry.error?"任务未加载，请刷新重试":"正在读取任务…"));}
        else if(!entry.tasks.length){group.appendChild(ui.mk("p","empty","当前账号没有成长任务"));}
        entry.tasks.slice().sort(function(a,b){return (a.task_code==="first_buddy"&&!a.claimed?-1:0)-(b.task_code==="first_buddy"&&!b.claimed?-1:0);}).forEach(function(task){
          var item=ui.mk("div","growth-task"),content=ui.mk("div","growth-task-content");item.dataset.taskCode=task.task_code;
          content.appendChild(ui.mk("strong","growth-task-title",task.title||task.task_code));
          var description=[task.task_desc,task.description].filter(function(s,i,a){return s&&a.indexOf(s)===i;}).join("；");
          if(description)content.appendChild(ui.mk("p","growth-task-description",description));
          if(!task.claimed&&(task.action_description||task.manual_instruction))content.appendChild(ui.mk("p","growth-task-description",task.action_description||task.manual_instruction));
          var meta=ui.mk("div","growth-task-meta");
          meta.appendChild(ui.mk("span","",task.target>0?"进度 "+task.current+" / "+task.target:"进度待确认"));
          var rewards=[];
          if(task.credit)rewards.push("+"+task.credit+" 积分");
          if(task.energy)rewards.push("+"+task.energy+" 能量");
          if(task.reward_buddy)rewards.push("Buddy 奖励");
          meta.appendChild(ui.mk("span","growth-task-reward",rewards.join(" · ")||"奖励以上游活动为准"));
          var status=task.claimed?"已领取":(task.task_code==="first_buddy"&&entry.buddy&&entry.buddy.owned?"已领养":(task.locked?"未解锁":(entry.running===task.task_code?"执行中":(entry.rejected.has(task.task_code)?"操作未完成":(entry.blocked.has(task.task_code)||task.execution_pending?"待刷新确认":(task.claimable?"可领取":(task.accept_status==="not_accepted"&&!task.accept_not_required?"未报名":"未完成")))))));
          meta.appendChild(ui.pill(status,task.claimed?"ok":(task.claimable&&!entry.blocked.has(task.task_code)?"warn":"")));
          content.appendChild(meta);item.appendChild(content);
          var actions=ui.mk("div","growth-task-actions");
          if(!task.accept_not_required&&task.accept_status==="not_accepted"){
            var accept=ui.btn("接取任务","xs",function(){return operate([{entry:entry,codes:[task.task_code]}],"accept");});
            accept.disabled=busy||!eligible(entry,task,"accept");actions.appendChild(accept);
          }
          if(task.auto_supported){
            var run=ui.btn(task.task_code==="first_buddy"?"解锁并领养":"执行任务","xs",function(){return runTask(entry,task.task_code);});
            run.disabled=busy||!eligible(entry,task,"run");actions.appendChild(run);
          }
          var button=ui.btn("领取奖励","xs",function(){return operate([{entry:entry,codes:[task.task_code]}],"claim");});
          button.disabled=busy||!claimable(entry,task);actions.appendChild(button);item.appendChild(actions);group.appendChild(item);
        });
        list.appendChild(group);
      });
    }
    function load(){
      if(busy)return Promise.resolve();
      var seq=++sequence;
      $("growthBuddyAgree").checked=false;
      entries=accounts.filter(function(a){return !select.value||a.id===select.value;}).map(function(a){return {account:a,tasks:[],buddy:null,loaded:false,error:"",blocked:new Set(),acceptBlocked:new Set(),rejected:new Set()};});
      $("growthAccountName").textContent=select.value?((accounts.find(function(a){return a.id===select.value;})||{}).name||"WorkBuddy"):"全部 WorkBuddy 账号";
      busy=true;busyKind="refresh";notice="正在刷新任务进度…";render();
      return workers(entries,seq,async function(entry){
        try{
          var r=await ui.api("/admin/api/workbuddy/tasks?id="+encodeURIComponent(entry.account.id));
          if(active(seq)){entry.tasks=r.tasks||[];entry.buddy=r.buddy||null;entry.loaded=true;entry.error=r.buddy_error||"";}
        }catch(e){if(active(seq))entry.error="查询失败："+e.message;}
        if(active(seq))render();
      }).finally(function(){if(active(seq)){
        busy=false;busyKind="";
        var tasks=entries.flatMap(function(e){return e.tasks;});
        notice=entries.some(function(e){return !e.loaded;})?"部分任务未能读取，请查看原因并刷新重试":"刷新完成："+tasks.length+" 项任务，"+tasks.filter(function(t){return t.claimable;}).length+" 项可领取。";
        render();
      }});
    }
    async function operate(jobs,kind){
      if(busy||!jobs.length)return;
      if(kind==="run"&&!$("growthBuddyAgree").checked&&jobs.some(function(job){return needsConsent(job.entry);})){jobs.forEach(function(job){if(needsConsent(job.entry))job.entry.error="请先阅读官方活动规则，并勾选同意领养协议";});notice="尚未开始执行，未产生对话消耗。";render();$("growthBuddyAgree").focus();return;}
      var seq=sequence,confirmed=0,total=jobs.reduce(function(n,job){return n+job.codes.length;},0);
      busy=true;busyKind=kind;notice=kind==="accept"?"正在接取任务…":(kind==="run"?"正在执行成长任务并核对进度，请稍候…":"正在领取已达标奖励…");
      controller=kind==="run"?new AbortController():null;
      var signal=controller?controller.signal:undefined;
      jobs.forEach(function(job){job.entry.error="";});render();
      try{
        await workers(jobs,seq,async function(job){
          var entry=job.entry,id=entry.account.id;
          for(var i=0;i<job.codes.length&&active(seq);i+=kind==="accept"?20:1){
            var codes=kind==="accept"?job.codes.slice(i,i+20):[job.codes[i]],code=codes[0];
            entry.running=kind==="run"?code:"";render();
            try{
              var body=kind==="accept"?{id:id,task_codes:codes}:{id:id,task_code:code};
              if(kind==="run"&&$("growthBuddyAgree").checked)body.agree_buddy=true;
              var r=await ui.api("/admin/api/workbuddy/tasks/"+kind,{method:"POST",body:body,signal:signal});
              if(r.balance)ui.onBalance(id,r.balance);
              if(!active(seq))break;
              entry.tasks=r.tasks||entry.tasks;
              if(r.buddy)entry.buddy=r.buddy;
              var prepared=kind==="run"&&code==="first_buddy"&&r.state==="prepared"&&entry.buddy&&entry.buddy.owned;
              if(kind==="accept")confirmed+=codes.filter(function(c){return accepted(entry,c);}).length;
              else if(r.state==="claimed"||r.state==="already_claimed"||prepared)confirmed++;
              if(r.state!=="claimed"&&r.state!=="already_claimed"&&!prepared&&!(kind==="accept"&&r.state==="accepted")){
                codes.forEach(function(c){if(kind!=="accept"||!accepted(entry,c))block(entry,c,kind);});entry.error=r.message||"操作结果待确认，请刷新任务进度";break;
              }
              if(r.balance_error)entry.error=r.balance_error;
            }catch(e){
              if(active(seq)){
                entry.error=e.message;
                // A rejected batch may already contain successful enrollments.
                // Read once, without retrying the write or starting any chat.
                if(kind==="accept"){
                  try{
                    var updated=await ui.api("/admin/api/workbuddy/tasks?id="+encodeURIComponent(id));
                    if(!active(seq))break;
                    entry.tasks=updated.tasks||entry.tasks;
                    if(updated.buddy)entry.buddy=updated.buddy;
                  }catch(readError){if(active(seq))entry.error+="；接取后回查失败，请刷新进度核对";}
                }
                if(active(seq))codes.forEach(function(c){
                  if(kind==="accept"&&accepted(entry,c)){confirmed++;return;}
                  block(entry,c,kind);
                  if(e.status>=400&&e.status<500&&e.status!==408)entry.rejected.add(c);
                });
              }
              break;
            }finally{entry.running="";if(active(seq))render();}
          }
        });
      }finally{if(active(seq)){
        busy=false;busyKind="";controller=null;
        notice=(kind==="accept"?"接取":(kind==="run"?"执行":"领取"))+"结束：已确认 "+confirmed+" 项"+(confirmed<total?"，"+(total-confirmed)+" 项未完成，请查看原因。":"。");
        render();
      }}
    }
    $("growthRefresh").onclick=load;
    select.onchange=load;
    $("growthClaimAll").onclick=function(){return operate(selectedJobs("claim"),"claim");};
    $("growthAcceptAll").onclick=function(){return operate(selectedJobs("accept"),"accept");};
    $("growthRunAll").onclick=function(){return operate(selectedJobs("run"),"run");};
    $("growthClose").onclick=function(){dialog.close();};
    dialog.addEventListener("close",function(){sequence++;if(controller)controller.abort();controller=null;accounts=[];entries=[];busy=false;});
    function openAccounts(list){
      sequence++;busy=false;entries=[];
      accounts=list.map(function(a){return {id:a.id,name:a.name,enabled:a.enabled};});
      select.textContent="";
      if(accounts.length>1){var all=ui.mk("option","","全部账号（"+accounts.length+"）");all.value="";select.appendChild(all);}
      accounts.forEach(function(a){var option=ui.mk("option","",a.name||"WorkBuddy");option.value=a.id;select.appendChild(option);});
      $("growthAccountPicker").classList.toggle("hidden",accounts.length<2);
      if(!dialog.open)dialog.showModal();
      return load();
    }
    return {open:function(a){return openAccounts([a]);},openAccounts:openAccounts};
  };
})();
