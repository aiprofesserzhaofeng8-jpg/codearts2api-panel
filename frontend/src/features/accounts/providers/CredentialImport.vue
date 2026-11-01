<template>
<section v-once id="manualAccountPanel" class="account-authorize-panel hidden">
          <div v-once class="card-head">
            <div v-once>
              <h3 class="account-panel-title">码道凭据导入</h3>
              <p class="card-sub">手工录入 STS 临时凭据，或从 session JSON 导入完整登录态</p>
            </div>
          </div>

          <div v-once class="row"><button class="btn" id="importLocal">从本机码道客户端导入</button></div>
          <div v-once class="settings-group first">
            <h3>手工录入</h3>
            <div v-once class="field-grid cols-2">
              <div v-once class="field">
                <label for="manualName">账号名称（可选）</label>
                <input id="manualName" placeholder="留空自动命名">
              </div>
              <div v-once class="field">
                <label for="manualKind">凭据类型</label>
                <select id="manualKind">
                  <option value="sts">STS 临时凭据（约 2 小时，可自动续期）</option>
                  <option value="aksk">永久 AK/SK（不过期，不可续期）</option>
                </select>
              </div>
              <div v-once class="field">
                <label for="manualAK">Access Key</label>
                <input id="manualAK" placeholder="STS 临时 AK 或 IAM 永久 AK">
              </div>
              <div v-once class="field">
                <label for="manualSK">Secret Key</label>
                <input id="manualSK">
              </div>
              <div v-once class="field" id="manualSTSField">
                <label for="manualSTS">Security Token（可选）</label>
                <input id="manualSTS">
              </div>
            </div>
            <p class="hint" id="manualKindHint">STS 临时凭据约 2 小时过期，到期需重新录入或用 refresh_token 续期。</p>
            <div v-once class="row mt-12">
              <button class="btn primary" id="addManual">录入账号</button>
            </div>
          </div>

          <div v-once class="settings-group">
            <h3>从 session JSON 导入</h3>
            <div v-once class="field-grid cols-2">
              <div v-once class="field">
                <label for="sessionName">账号名称（可选）</label>
                <input id="sessionName" placeholder="留空自动命名">
              </div>
              <div v-once class="field">
                <label for="sessionJSON">session JSON</label>
                <input id="sessionJSON" placeholder="HuaweiCloudSession 解密后的内容">
              </div>
            </div>
            <div v-once class="row mt-12">
              <button class="btn" id="addSession">导入账号</button>
            </div>
          </div>
        </section>
</template>
