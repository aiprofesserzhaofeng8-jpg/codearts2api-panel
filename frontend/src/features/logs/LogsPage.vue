<template>
<section v-once id="tab-logs" role="tabpanel" aria-labelledby="nav-logs">
        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>请求日志</h2>
              <p class="card-sub">只记录元信息（模型、耗时、长度），不落对话内容</p>
            </div>
            <div v-once class="card-actions">
              <select id="logLimit" class="w-auto" aria-label="日志条数">
                <option value="50">最近 50</option>
                <option value="200">最近 200</option>
                <option value="0">全部</option>
              </select>
              <button class="btn" id="reloadLogs">刷新</button>
              <button class="btn danger" id="clearLogs">清空</button>
            </div>
          </div>
          <div v-once class="table-wrap">
            <table>
              <thead><tr><th>时间</th><th>模型</th><th>流式</th><th>状态</th><th title="上游首个有效分片 / 请求总耗时">首段 / 总耗时</th><th>入/出</th><th>账号</th><th>密钥</th><th>错误</th></tr></thead>
              <tbody id="logs"></tbody>
            </table>
          </div>
        </div>
      </section>
</template>
