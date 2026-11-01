<template>
<section v-once id="tab-groups" role="tabpanel" aria-labelledby="nav-groups">
        <div v-once class="card">
          <div v-once class="card-head"><div><h2>账号分组</h2></div><button class="btn primary" id="addGroup">创建分组</button></div>
          <div v-once class="table-wrap"><table><thead><tr><th>名称</th><th>账号数</th><th>模型范围</th><th>绑定密钥</th><th>操作</th></tr></thead><tbody id="groups"></tbody></table></div>
        </div>
      </section>
<dialog v-once id="groupDialog" class="modal" aria-labelledby="groupTitle">
  <div v-once class="modal-head"><h2 id="groupTitle">账号分组</h2><button class="btn ghost" id="groupClose">关闭</button></div>
  <div v-once class="modal-body">
    <div v-once class="field"><label for="groupName">分组名称</label><input id="groupName" maxlength="80" placeholder="例如：日常、测试、Trae 专用"></div>
    <p class="account-field-label">可用账号</p><div id="groupAccounts" class="group-account-options"></div>
    <label class="check mt-14"><input id="groupAllowlist" type="checkbox">仅允许选中的模型</label>
    <div v-once id="groupModelsField" class="field mt-12"><label for="groupModels">模型白名单（可多选）</label><select id="groupModels" multiple size="8" aria-describedby="groupModelsHint"></select></div>
    <p id="groupModelsHint" class="hint">白名单限制可调用的原始型号。启用后未选型号则禁止调用。</p>
    <div v-once class="group-mapping-head"><h3>统一模型</h3><button class="btn" id="addGroupMapping">添加映射</button></div>
    <p class="hint">公开名称须带版本号，例如 deepseek-4.1-flash。选择多个平台的对应型号后，由账号池自动选号；每个平台和地区可选一个型号。</p>
    <div v-once id="groupMappingRows"></div>
    <p class="hint">配置后，此分组的客户端模型列表仅展示统一名称。原始型号仍可在分组权限内用完整平台 ID 调用。</p>
    <p id="groupError" class="err-text" role="alert"></p>
  </div>
  <div v-once class="modal-actions"><button class="btn" id="groupCancel">取消</button><button class="btn primary" id="groupSave">保存分组</button></div>
</dialog>
</template>
