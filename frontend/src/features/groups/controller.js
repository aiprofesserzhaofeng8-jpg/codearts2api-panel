import { t as tr } from '../../i18n';
import { cell, mk } from '../../components/controls';
import { serverState } from '../../shared/read-model';
import { domState } from '../../shared/dom';

// Only legacy key/model filters belong to this controller. GroupsPage owns its
// complete reactive editor and table; no native listener touches its DOM.
export function groupModelAllowed(model, group, data) {
 const aliases=(data.settings||{}).model_aliases||{};
 const resolved=model.source==='alias' ? (data.models||[]).find(m=>m.id===aliases[model.id])||model : model;
 if(group.allowlist_enabled&&!(group.model_ids||[]).includes(resolved.id))return false;
 return (data.accounts||[]).some(a=>(group.account_ids||[]).includes(a.id)&&a.enabled&&(a.provider||'codearts')===(resolved.provider||'codearts')&&((resolved.provider||'codearts')==='codearts'||(a.scope||'')===(resolved.scope||'')));
}
export function fillGroupSelect(select,value,first){
 select.textContent='';const option=mk('option','',first);option.value='';select.appendChild(option);
 const groups=serverState.data?.groups||[];
 groups.forEach(g=>{const o=mk('option','',g.name);o.value=g.id;select.appendChild(o);});
 if(value&&!groups.some(g=>g.id===value)){const missing=mk('option','',tr('分组不存在，请重新选择'));missing.value=value;missing.disabled=true;select.appendChild(missing);}
 select.value=value||'';
}
export function keyGroupCell(key){const result=cell(key.name),group=serverState.data?.groups?.find(g=>g.id===key.group_id);result.appendChild(mk('div','sub',key.group_id?(group?group.name:tr('分组不存在')):tr('不分组')));return result;}
export function renderGroups(){fillGroupSelect(domState.$('#keyCreateGroup'),domState.$('#keyCreateGroup').value,tr('不分组（全部账号）'));fillGroupSelect(domState.$('#modelsGroupFilter'),domState.$('#modelsGroupFilter').value,tr('全部分组'));}
