<script setup lang="ts">
import { t as tr } from '../../i18n';
</script>
<template>
<section v-once id="tab-logs" role="tabpanel" aria-labelledby="nav-logs">
        <div v-once class="card">
          <div v-once class="card-head">
            <div v-once>
              <h2>{{tr('请求日志')}}</h2>
              <p class="card-sub">{{tr('只记录元信息（模型、耗时、长度），不落对话内容')}}</p>
            </div>
            <div v-once class="card-actions">
              <select id="logLimit" class="w-auto" :aria-label="tr('日志条数')">
                <option value="50">{{tr('最近 50')}}</option>
                <option value="200">{{tr('最近 200')}}</option>
                <option value="0">{{tr('全部')}}</option>
              </select>
              <button class="btn" id="reloadLogs">{{tr('刷新')}}</button>
              <button class="btn danger" id="clearLogs">{{tr('清空')}}</button>
            </div>
          </div>
          <div v-once class="table-wrap">
            <table>
              <thead><tr><th>{{tr('时间')}}</th><th>{{tr('模型')}}</th><th>{{tr('流式')}}</th><th>{{tr('状态')}}</th><th :title="tr('上游首个有效分片 / 请求总耗时')">{{tr('首段 / 总耗时')}}</th><th>{{tr('入/出')}}</th><th>{{tr('账号')}}</th><th>{{tr('密钥')}}</th><th>{{tr('错误')}}</th></tr></thead>
              <tbody id="logs"></tbody>
            </table>
          </div>
        </div>
      </section>
</template>
