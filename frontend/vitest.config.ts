import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';
// The embedded helper has one source outside frontend. Vite 8 checks the raw
// query ID as well as the actual file; keep both grants limited to that file.
export default defineConfig({server:{fs:{allow:['.','../scripts/teleagent-login.mjs','../scripts/teleagent-login.mjs?raw']}},plugins:[vue()],test:{include:['src/**/*.spec.ts'],environment:'jsdom',setupFiles:['src/tests/setup.ts']}});
