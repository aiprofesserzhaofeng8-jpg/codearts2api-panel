<script setup lang="ts">
import { t as tr } from '../../i18n';

import { computed, ref } from 'vue';
import UserIcon from './UserIcon.vue';

const props = defineProps<{ models: { id: string; name: string }[] }>();
const query = ref('');
const copied = ref('');
const error = ref('');
const filtered = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  return props.models.filter(model => (model.id + ' ' + model.name).toLowerCase().includes(keyword));
});

async function copy(id: string) {
  error.value = '';
  try {
    await navigator.clipboard.writeText(id);
    copied.value = id;
  } catch {
    error.value = tr('请选中模型名称手动复制。');
  }
}
</script>

<template>
  <section id="userModels" class="portal-stack">
    <div class="portal-page-heading">
      <div><h2>{{tr('模型目录')}}</h2><p>{{tr('查看当前分组展示的模型，复制模型名称用于配置客户端。')}}</p></div>
      <span class="portal-badge">{{ models.length }}{{tr(' 个模型')}}</span>
    </div>
    <label class="portal-search">
      <UserIcon name="search" />
      <input v-model="query" type="search" :aria-label="tr('搜索模型')" :placeholder="tr('搜索模型名称…')">
    </label>
    <p v-if="error" class="portal-notice" role="status">{{ error }}</p>
    <div class="portal-model-grid">
      <article v-for="model in filtered" :key="model.id" class="portal-card portal-model-card">
        <span class="portal-model-icon"><UserIcon name="model" /></span>
        <h3>{{ model.name || model.id }}</h3>
        <code>{{ model.id }}</code>
        <div>
          <span class="portal-badge">{{tr('分组模型')}}</span>
          <button class="btn ghost xs" :aria-label="tr('复制模型名 ') + model.id" @click="copy(model.id)">
            <UserIcon :name="copied === model.id ? 'check' : 'copy'" />
            {{ copied === model.id ? tr('已复制') : tr('复制名称') }}
          </button>
        </div>
      </article>
    </div>
    <div v-if="!filtered.length" class="portal-card portal-empty">
      <span class="portal-empty-icon"><UserIcon name="search" /></span>
      <h3>{{ query ? tr('没有匹配的模型') : tr('暂无显示的模型') }}</h3>
      <p>{{ query ? tr('尝试其他名称或关键词。') : tr('请联系管理员检查分组配置。') }}</p>
    </div>
    <p class="portal-note">{{tr('展示与调用权限由管理员分别配置；模型隐藏不等于禁止调用。')}}</p>
  </section>
</template>
