import { mount } from '@vue/test-utils';
import { expect, it } from 'vitest';
import UserDashboard from '../features/users/UserDashboard.vue';
import type { components } from '../api/generated/admin';

it('shows usage coverage, model distribution and daily counts from the whole selected range', () => {
  const stats: components['schemas']['UserUsageStats'] = {
    total_requests: 24, failed_requests: 6, input_tokens: 240, output_tokens: 48,
    cached_tokens: 120, reasoning_tokens: 0, usage_requests: 23, priced_usd_requests: 20,
    total_latency_ms: 24000, latency_samples: 24, partial_usage_requests: 1, estimated_usd: .12,
  };
  const wrapper = mount(UserDashboard, { props: { usage: {
    stats, logs: [], page: 2, pages: 3, total: 24, retention_limit: 100000,
    analysis: { time_zone: 'Asia/Shanghai', bucket_days: 1,
      trend: [{ time: 1735689600, stats }], models: [{ name: 'public-4.1', stats }] },
  } } });
  expect(wrapper.text()).toContain('75.0%');
  expect(wrapper.text()).toContain('1000 ms');
  expect(wrapper.text()).toContain('288');
  expect(wrapper.text()).toContain('$0.120000');
  expect(wrapper.text()).toContain('public-4.1');
  expect(wrapper.text()).toContain('已报告用量 23 次');
  expect(wrapper.get('meter').attributes('value')).toBe('24');
});

it('keeps absent history distinct from a measured zero rate', () => {
  const wrapper = mount(UserDashboard);
  expect(wrapper.text()).toContain('当前范围暂无请求记录');
  expect(wrapper.text()).toContain('当前范围暂无模型用量');
  expect(wrapper.text()).not.toContain('100.0%');
});
