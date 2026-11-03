import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';
export default defineConfig({plugins:[vue()],test:{include:['src/**/*.spec.ts'],environment:'jsdom',setupFiles:['src/tests/setup.ts']}});
