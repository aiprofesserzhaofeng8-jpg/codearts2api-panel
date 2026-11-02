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
    <div v-once class="group-mapping-head"><h3>统一模型</h3><div class="actions"><button class="btn" id="applyGroupPresets">应用映射预设</button><button class="btn" id="addGroupMapping">添加映射</button></div></div>
    <p class="hint">公开名称须带版本号，例如 deepseek-4.1-flash。选择多个平台的对应型号后，由账号池自动选号；每个平台和地区可选一个型号。</p>
    <div v-once id="groupMappingRows"></div>
    <p class="hint">预设只添加当前账号与白名单内、目录已确认的来源，不覆盖已有映射。保存后固定来源。</p>
    <div class="field-grid cols-2"><div class="field"><label for="groupDefaultModel">请求默认模型</label><select id="groupDefaultModel"></select><p class="hint">仅请求未填写 model 时使用；留空沿用全局默认。</p></div><div class="field"><label for="groupClientDefaultModel">客户端推荐模型</label><select id="groupClientDefaultModel"></select><p class="hint">供支持的客户端预选，不改变请求路由。</p></div></div>
    <details><summary>模型显示与调用权限</summary><p class="hint">显示控制模型列表；允许调用控制请求权限。隐藏后仍可按名称调用。禁用来源同时影响引用它的统一模型。</p><div class="table-wrap"><table><thead><tr><th>模型</th><th>显示</th><th>允许调用</th></tr></thead><tbody id="groupModelRules"></tbody></table></div></details>
    <p id="groupError" class="err-text" role="alert"></p>
  </div>
  <div v-once class="modal-actions"><button class="btn" id="groupCancel">取消</button><button class="btn primary" id="groupSave">保存分组</button></div>
</dialog>
</template>
