// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { it, expect, vi } from 'vitest';
import SourceStatusPanel from '../features/models/SourceStatusPanel.vue';
import type { components } from '../api/generated/admin';
it('keeps capability, scheduling, requests, probes and quota independent without fetching',()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch);
 const status:components['schemas']['UnifiedSourceStatus']={id:'qoder/cn/fast',catalog_status:'present',enabled:true,eligible_account_count:0,last_probe:null,observed_at:'2026-11-07T12:00:00Z',image_input:'supported',tool_calling:'',runtime:{account_count:2,ready:0,disabled:1,paused:0,cooling:1,busy:0,next_retry_at:1794052800,last_request:{checked_at:1794050000,status:429,error_kind:'rate_limited'}},quota:[{account_id:'fixture',observed_at:'2026-11-07T11:00:00Z',stale:true,available:true,snapshot:{available:true,kind:'subscription_windows',windows:[{id:'week',name:'7 天窗口',utilization:100}]}}]};
 const wrapper=mount(SourceStatusPanel,{props:{status}});const text=wrapper.text();
 expect(text).toContain('图片 支持');expect(text).toContain('工具 未知');expect(text).toContain('HTTP 429');expect(text).toContain('不代表配额重置时间');expect(text).toContain('文本测活记录: 尚无记录');expect(text).toContain('已过期');expect(text).toContain('重置时间: 未知');expect(fetch).not.toHaveBeenCalled();wrapper.unmount();vi.unstubAllGlobals();
});
it('shows absent quota as unknown instead of a zero balance',()=>{
 const status={id:'missing',catalog_status:'missing',enabled:true,eligible_account_count:0,last_probe:null,observed_at:'2026-11-07T12:00:00Z',quota:[]} as components['schemas']['UnifiedSourceStatus'];
 const wrapper=mount(SourceStatusPanel,{props:{status}});expect(wrapper.text()).toContain('未知，请在账号页刷新额度');expect(wrapper.text()).not.toContain('剩余额度: 0');wrapper.unmount();
});
it('distinguishes tested client recommendations from provider maxima and specialized purposes',()=>{
 const status:components['schemas']['UnifiedSourceStatus']={id:'qoder/cn/gfmodel',catalog_status:'present',enabled:true,eligible_account_count:1,last_probe:null,observed_at:'2026-11-07T12:00:00Z',quota:[],client_compatibility:{purpose:'chat',exportable:true,status:'ready',reason:'',limits:[{source:'qoder/cn/gfmodel',basis:'tested_recommendation',context_window:32768,max_output_tokens:4096,verified_at:'2026-11-07'}]}};
 const wrapper=mount(SourceStatusPanel,{props:{status}});
 expect(wrapper.text()).toContain('通用聊天');expect(wrapper.text()).toContain('可导出到聊天客户端');expect(wrapper.text()).toContain('已验证的保守建议');expect(wrapper.text()).toContain('32768 / 4096');expect(wrapper.text()).not.toContain('上游目录上限: 32768');expect(wrapper.text()).toContain('不代表官方或平台最大值');wrapper.unmount();
 const specialized=mount(SourceStatusPanel,{props:{status:{...status,client_compatibility:{...status.client_compatibility!,purpose:'image_generation',exportable:false,status:'unsupported_purpose'}}}});
 expect(specialized.text()).toContain('图片生成');expect(specialized.text()).toContain('暂不自动导出');specialized.unmount();
});

it('labels public references and defaults separately from upstream maxima',()=>{
 for(const [basis,label] of [['public_registry','公开模型资料'],['compatibility_default','兼容默认参数']]){
  const status:components['schemas']['UnifiedSourceStatus']={id:'teleagent/cn/chat-pro',catalog_status:'present',enabled:true,eligible_account_count:0,last_probe:null,observed_at:'2026-11-07T12:00:00Z',quota:[],client_compatibility:{purpose:'chat',exportable:true,status:'ready',reason:'',limits:[{source:'teleagent/cn/chat-pro',basis,context_window:32768,max_output_tokens:4096}]}};
  const wrapper=mount(SourceStatusPanel,{props:{status}});expect(wrapper.text()).toContain(label);expect(wrapper.text()).toContain('32768 / 4096');expect(wrapper.text()).not.toContain('上游目录上限: 32768');wrapper.unmount();
 }
});
