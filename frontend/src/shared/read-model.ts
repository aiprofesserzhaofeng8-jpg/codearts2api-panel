import type { components } from "../api/generated/admin";
import { shallowReactive } from 'vue';
export const serverState = shallowReactive<{ data: components["schemas"]["State"] | null }>({ data: null });
