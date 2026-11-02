<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { components } from '../../api/generated/admin';
import { prepareAvatar } from './avatar';
type User = components['schemas']['User'];
type Update = components['schemas']['UserProfileUpdate'];
const props = defineProps<{ user: User; busy: boolean }>();
const emit = defineEmits<{ save: [update: Update] }>();
const name = ref(''), avatar = ref(''), error = ref(''), preparing = ref(false);
let upload = 0;
watch(() => [props.user.display_name, props.user.avatar], () => {
  upload++; name.value = props.user.display_name; avatar.value = props.user.avatar; error.value = ''; preparing.value = false;
}, { immediate: true });
const dirty = computed(() => name.value !== props.user.display_name || avatar.value !== props.user.avatar);
async function select(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0];
  input.value = '';
  if (!file) return;
  const current = ++upload;
  preparing.value = true; error.value = '';
  try { const value = await prepareAvatar(file); if (current === upload) avatar.value = value; }
  catch (e) { if (current === upload) error.value = (e as Error).message; }
  finally { if (current === upload) preparing.value = false; }
}
function save() {
  const update: Update = {};
  if (name.value !== props.user.display_name) update.display_name = name.value;
  if (avatar.value !== props.user.avatar) update.avatar = avatar.value;
  emit('save', update);
}
</script>
<template>
  <section class="portal-card" id="userProfile">
    <div class="portal-section-heading"><div><h2>资料与头像</h2><p>维护公开展示信息，设置你的头像与昵称。</p></div></div>
    <form class="portal-profile-grid" @submit.prevent="save">
      <div class="user-avatar-editor">
        <img v-if="avatar" :src="avatar" class="user-avatar large" alt="头像预览">
        <span v-else class="user-avatar large" aria-label="默认头像">{{(name||user.username).slice(0,1).toUpperCase()}}</span>
        <h3>资料头像</h3><p class="portal-muted">静态图片自动压缩到 20KB 以内；GIF 须不超过 20KB。</p>
        <div class="portal-inline-actions">
        <label class="btn user-upload">上传图片<input id="userAvatarFile" type="file" accept="image/png,image/jpeg,image/gif" :disabled="busy||preparing" @change="select"></label>
        <button class="btn" id="userAvatarRemove" type="button" :disabled="busy||preparing||!avatar" @click="avatar=''">删除头像</button>
        </div>
        <p v-if="preparing" role="status">正在处理图片…</p>
      </div>
      <div class="portal-profile-form"><h3>编辑个人资料</h3><div class="field"><label for="userDisplayName">昵称</label><input id="userDisplayName" v-model="name" maxlength="40" :disabled="busy||preparing" placeholder="留空使用登录用户名"></div>
        <p class="portal-note">登录用户名为 {{user.username}}，修改昵称不会更改登录账号。</p>
        <button class="btn primary" id="userProfileSave" :disabled="busy||preparing||!dirty">保存资料</button>
      </div>
    </form>
    <p v-if="error" class="err-text" role="alert">{{error}}</p>
  </section>
</template>
