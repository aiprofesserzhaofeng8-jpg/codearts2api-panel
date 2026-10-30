// Account-scoped task UI. No background polling or changes to other views.
(function(){
  "use strict";
  window.CodeArtsGrowth=function(ui){
    var $=function(id){return document.getElementById(id);};
    var dialog=$("growthDialog"),sequence=0,account=null,tasks=[],busy=false,loaded=false,blocked=new Set();
    function active(seq,id){return sequence===seq&&account&&account.id===id&&dialog.open;}
    function claimable(t){return account&&account.enabled&&t.claimable&&!blocked.has(t.task_code);}
    function render(){
      $("growthRefresh").disabled=busy;
      $("growthClaimAll").disabled=busy||!loaded||!tasks.some(claimable);
      var list=$("growthTaskList");list.setAttribute("aria-busy",busy?"true":"false");list.textContent="";
      if(!loaded){list.appendChild(ui.mk("p","hint",busy?"正在读取任务…":"请刷新任务进度"));return;}
      $("growthSummary").textContent=tasks.length+" 项任务 · "+tasks.filter(function(t){return t.claimed;}).length+" 项已领取 · "+tasks.filter(function(t){return t.claimable;}).length+" 项可领取";
      if(!tasks.length){list.appendChild(ui.mk("p","empty","当前账号没有成长任务"));return;}
      tasks.forEach(function(t){
        var item=ui.mk("div","growth-task"),content=ui.mk("div","growth-task-content");
        item.dataset.taskCode=t.task_code;
        content.appendChild(ui.mk("strong","growth-task-title",t.title||t.task_code));
        var description=[t.task_desc,t.description].filter(function(s,i,a){return s&&a.indexOf(s)===i;}).join("；");
        if(description)content.appendChild(ui.mk("p","growth-task-description",description));
        var meta=ui.mk("div","growth-task-meta");
        meta.appendChild(ui.mk("span","",t.target>0?"进度 "+t.current+" / "+t.target:"进度待确认"));
        var rewards=[];
        if(t.credit)rewards.push("+"+t.credit+" 积分");
        if(t.energy)rewards.push("+"+t.energy+" 能量");
        if(t.reward_buddy)rewards.push("Buddy 奖励");
        meta.appendChild(ui.mk("span","growth-task-reward",rewards.join(" · ")||"奖励以上游活动为准"));
        var status=t.claimed?"已领取":(t.locked?"未解锁":(blocked.has(t.task_code)?"待刷新确认":(t.claimable?"可领取":(t.accept_status==="not_accepted"?"未报名":"未完成"))));
        meta.appendChild(ui.pill(status,t.claimed?"ok":(t.claimable&&!blocked.has(t.task_code)?"warn":"")));
        content.appendChild(meta);item.appendChild(content);
        var button=ui.btn("领取奖励","xs",function(){return claim([t.task_code]);});
        button.disabled=busy||!claimable(t);item.appendChild(button);list.appendChild(item);
      });
    }
    function load(){
      if(!account||busy)return Promise.resolve();
      var seq=++sequence,id=account.id;
      busy=true;loaded=false;tasks=[];blocked.clear();$("growthError").textContent="";$("growthSummary").textContent="";render();
      return ui.api("/admin/api/workbuddy/tasks?id="+encodeURIComponent(id)).then(function(r){
        if(!active(seq,id))return;
        tasks=r.tasks||[];loaded=true;
      }).catch(function(e){
        if(active(seq,id))$("growthError").textContent="查询失败："+e.message;
      }).finally(function(){if(active(seq,id)){busy=false;render();}});
    }
    async function claim(codes){
      if(!account||busy||!loaded)return;
      var seq=sequence,id=account.id;
      busy=true;$("growthError").textContent="";render();
      try{
        var confirmed=0;
        for(var i=0;i<codes.length;i++){
          if(!active(seq,id))break;
          var code=codes[i],r;
          try{r=await ui.api("/admin/api/workbuddy/tasks/claim",{method:"POST",body:{id:id,task_code:code}});}
          catch(e){if(active(seq,id))blocked.add(code);throw e;}
          if(r.balance)ui.onBalance(id,r.balance);
          if(!active(seq,id))break;
          tasks=r.tasks||tasks;
          if(r.state!=="claimed"&&r.state!=="already_claimed"){
            blocked.add(code);$("growthError").textContent=r.message||"领取结果待确认，请刷新任务进度";break;
          }
          confirmed++;
          $("growthSummary").textContent="已确认 "+confirmed+" 项奖励";
          if(r.balance_error)$("growthError").textContent=r.balance_error;
        }
      }catch(e){if(active(seq,id))$("growthError").textContent=e.message;}
      finally{if(active(seq,id)){busy=false;render();}}
    }
    $("growthRefresh").onclick=load;
    $("growthClaimAll").onclick=function(){return claim(tasks.filter(claimable).map(function(t){return t.task_code;}));};
    $("growthClose").onclick=function(){dialog.close();};
    dialog.addEventListener("close",function(){sequence++;account=null;tasks=[];busy=false;loaded=false;blocked.clear();});
    return {open:function(a){
      sequence++;account={id:a.id,enabled:a.enabled};tasks=[];busy=false;loaded=false;blocked.clear();
      $("growthAccountName").textContent=a.name||"WorkBuddy";
      if(!dialog.open)dialog.showModal();
      return load();
    }};
  };
})();
