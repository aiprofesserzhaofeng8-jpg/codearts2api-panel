// Bounded account workers share the existing account-scoped read/claim APIs.
// Closing the dialog stops queued work; no timers or background queue remain.
(function(){
  "use strict";
  window.CodeArtsGrowth=function(ui){
    var $=function(id){return document.getElementById(id);};
    var dialog=$("growthDialog"),sequence=0,accounts=[],entries=[],busy=false;
    var select=$("growthAccountSelect");
    function active(seq){return sequence===seq&&dialog.open;}
    function claimable(entry,task){return entry.account.enabled&&entry.loaded&&task.claimable&&!entry.blocked.has(task.task_code);}
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
      var all=entries.flatMap(function(e){return e.tasks;}),failed=entries.filter(function(e){return e.error;});
      $("growthSummary").textContent=(entries.length>1?entries.length+" 个账号 · ":"")+all.length+" 项任务 · "+all.filter(function(t){return t.claimed;}).length+" 项已领取 · "+all.filter(function(t){return t.claimable;}).length+" 项可领取";
      $("growthError").textContent=failed.length===1&&entries.length===1?failed[0].error:(failed.length?failed.length+" 个账号出现错误，请查看对应账号结果":"");
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
        entry.tasks.forEach(function(task){
          var item=ui.mk("div","growth-task"),content=ui.mk("div","growth-task-content");item.dataset.taskCode=task.task_code;
          content.appendChild(ui.mk("strong","growth-task-title",task.title||task.task_code));
          var description=[task.task_desc,task.description].filter(function(s,i,a){return s&&a.indexOf(s)===i;}).join("；");
          if(description)content.appendChild(ui.mk("p","growth-task-description",description));
          var meta=ui.mk("div","growth-task-meta");
          meta.appendChild(ui.mk("span","",task.target>0?"进度 "+task.current+" / "+task.target:"进度待确认"));
          var rewards=[];
          if(task.credit)rewards.push("+"+task.credit+" 积分");
          if(task.energy)rewards.push("+"+task.energy+" 能量");
          if(task.reward_buddy)rewards.push("Buddy 奖励");
          meta.appendChild(ui.mk("span","growth-task-reward",rewards.join(" · ")||"奖励以上游活动为准"));
          var status=task.claimed?"已领取":(task.locked?"未解锁":(entry.blocked.has(task.task_code)?"待刷新确认":(task.claimable?"可领取":(task.accept_status==="not_accepted"?"未报名":"未完成"))));
          meta.appendChild(ui.pill(status,task.claimed?"ok":(task.claimable&&!entry.blocked.has(task.task_code)?"warn":"")));
          content.appendChild(meta);item.appendChild(content);
          var button=ui.btn("领取奖励","xs",function(){return claim([{entry:entry,codes:[task.task_code]}]);});
          button.disabled=busy||!claimable(entry,task);item.appendChild(button);group.appendChild(item);
        });
        list.appendChild(group);
      });
    }
    function load(){
      if(busy)return Promise.resolve();
      var seq=++sequence;
      entries=accounts.filter(function(a){return !select.value||a.id===select.value;}).map(function(a){return {account:a,tasks:[],loaded:false,error:"",blocked:new Set()};});
      $("growthAccountName").textContent=select.value?((accounts.find(function(a){return a.id===select.value;})||{}).name||"WorkBuddy"):"全部 WorkBuddy 账号";
      busy=true;render();
      return workers(entries,seq,async function(entry){
        try{
          var r=await ui.api("/admin/api/workbuddy/tasks?id="+encodeURIComponent(entry.account.id));
          if(active(seq)){entry.tasks=r.tasks||[];entry.loaded=true;}
        }catch(e){if(active(seq))entry.error="查询失败："+e.message;}
        if(active(seq))render();
      }).finally(function(){if(active(seq)){busy=false;render();}});
    }
    async function claim(jobs){
      if(busy)return;
      var seq=sequence;busy=true;jobs.forEach(function(job){job.entry.error="";});render();
      try{
        await workers(jobs,seq,async function(job){
          var entry=job.entry,id=entry.account.id;
          for(var i=0;i<job.codes.length&&active(seq);i++){
            var code=job.codes[i];
            try{
              var r=await ui.api("/admin/api/workbuddy/tasks/claim",{method:"POST",body:{id:id,task_code:code}});
              if(r.balance)ui.onBalance(id,r.balance);
              if(!active(seq))break;
              entry.tasks=r.tasks||entry.tasks;
              if(r.state!=="claimed"&&r.state!=="already_claimed"){
                entry.blocked.add(code);entry.error=r.message||"领取结果待确认，请刷新任务进度";break;
              }
              if(r.balance_error)entry.error=r.balance_error;
            }catch(e){
              if(active(seq)){entry.blocked.add(code);entry.error=e.message;}
              break;
            }finally{if(active(seq))render();}
          }
        });
      }finally{if(active(seq)){busy=false;render();}}
    }
    $("growthRefresh").onclick=load;
    select.onchange=load;
    $("growthClaimAll").onclick=function(){
      var jobs=entries.map(function(e){return {entry:e,codes:e.tasks.filter(function(t){return claimable(e,t);}).map(function(t){return t.task_code;})};}).filter(function(job){return job.codes.length;});
      return claim(jobs);
    };
    $("growthClose").onclick=function(){dialog.close();};
    dialog.addEventListener("close",function(){sequence++;accounts=[];entries=[];busy=false;});
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
