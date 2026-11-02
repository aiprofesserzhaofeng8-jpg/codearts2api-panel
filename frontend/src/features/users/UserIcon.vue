<script setup lang="ts">
defineProps<{name: string}>();
const paths: Record<string, string> = {
  activity:'M3 12h4l3-8 4 16 3-8h4',
  chart:'M4 19V9m6 10V4m6 15v-7m5 9H3',
  check:'m5 12 4 4L19 6',
  clock:'M12 8v4l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z',
  copy:'M9 9h11v11H9zM15 9V4H4v11h5',
  gift:'M3 8h18v4H3zM5 12v9h14v-9M12 8v13M12 8H8a3 3 0 1 1 3-3l1 3Zm0 0h4a3 3 0 1 0-3-3l-1 3Z',
  github:'M9 19c-4 1-4-2-6-2m12 5v-4c0-1 .1-2-.5-3 3-.3 6-1.4 6-6a5 5 0 0 0-1.4-3.5A4.6 4.6 0 0 0 19 2s-1.1-.4-3.6 1.3a13 13 0 0 0-6.5 0C6.4 1.6 5.3 2 5.3 2a4.6 4.6 0 0 0-.1 3.5A5 5 0 0 0 3.8 9c0 4.6 2.9 5.7 5.9 6-.5.5-.7 1.1-.7 2v5',
  key:'M11 12h10m-4 0v3m3-3v2M11 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
  link:'m10 13 4-4m-6 6-1 1a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m2-1 1-1a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0',
  lock:'M6 10h12v11H6zM8 10V6a4 4 0 0 1 8 0v4m-4 4v3',
  mail:'M3 5h18v14H3zM3 5l9 7 9-7',
  model:'m12 3 9 5-9 5-9-5 9-5ZM3 8v9l9 5 9-5V8M12 13v9',
  plus:'M12 5v14M5 12h14',
  refresh:'M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1',
  search:'m21 21-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z',
  shield:'m12 3-8 3v6c0 4 3 7 8 9 5-2 8-5 8-9V6l-8-3Zm-4 9 3 3 5-6',
  token:'m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Zm-8 4.5 8 5 8-5M12 12.5V21',
  user:'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21v-2a8 8 0 0 1 16 0v2',
  wallet:'M3 6h18v14H3zM3 10h18m-6 5h3M6 3h12',
};
</script>
<template><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path :d="paths[name]" /></svg></template>
