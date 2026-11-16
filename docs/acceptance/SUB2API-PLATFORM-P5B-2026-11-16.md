# P5b Vertex AI 第一批：服务账号认证基础

日期：2026-11-16。仅基础层已实现，不能称 Vertex AI 已可供用户使用。

## 范围

参考固定 Sub2API vertex_service_account.go：服务账号 RSA JWT、固定 Google OAuth 端点、cloud-platform scope、JWT bearer grant、项目/区域及 global 请求地址。标准库 RSA/x509 完成签名，无新增依赖。保留上游来源哈希、LGPL 许可及 Linux/Windows/Docker 分发声明。

Normalize 返回稳定的服务账号邮箱+目标项目+区域身份；私钥轮转不改变身份。凭据有效期来自真实 expires_in，缺失/非法值不伪造一小时。第一次无 access token 时可由原有凭据协调器换取；后续沿用五分钟刷新窗口与 Store CAS。不增加缓存、锁、后台任务或隐式主机认证。导入准备流程尚未接入，不能直接使用新凭据格式导入账号。

## 隔离验证

- 独立校验生成 JWT 的 RSA 签名、issuer、audience、scope、kid 和一小时生命周期。
- 自定义 token_uri 被规范到固定 Google 端点；非法私钥/项目/区域/凭据组合拒绝。
- 错误响应不泄漏描述；invalid_grant/invalid_client 标记认证失败，429 保留状态；无效 TTL、超大响应和取消请求覆盖。
- 8 个并发 Resolve 只执行一次换取，新令牌保存后重新打开 Store 仍存在。
- regional/global 主机和项目路径正确；非法型号/路径注入拒绝。

验证通过。竞态测试日志：/tmp/codearts-p5b-auth-race.log。全量 make check 通过，日志：/tmp/codearts-p5b-auth-check.log。真实云账号、真实 IAM、配额和计费尚未验证；测试仅使用生成的 RSA 私钥和模拟 HTTP。

## 后续

1. 复用 googlewire 接 Gemini 流式生成；目录可见不作为推理授权证明。
2. 接入现有导入验证、注册表、共享映射、权限/账本以及中英文账号 UI。
3. 覆盖三种客户端协议、工具、图片、取消与错误路径，随后用授权云账号真实验收。
4. API Key/Express、Vertex Claude、主机 ADC 不在本批实现范围，不能宣称支持。

本批未读取生产状态、未部署、未推送或发布。

## 第二批：平台、导入及生成接入

2026-11-16：已注册 Vertex AI 服务账号/Gemini 适配器，接入已有账号 JSON 导入、设置、手工共享映射、模型来源及中英文页面。上文“基础层未注册”为第一批历史状态。

- 复用 googlewire 完成原生 streamGenerateContent 生成，固定区域/global 主机和 Bearer 认证；不自动换模型/区域、不新增重试。
- 导入前可选 PrepareImport 在统一30秒期限内换取访问令牌，重新 Normalize 后复核身份和 scope；短推理通过才提交。任何准备失败、身份改变、凭据过期或保存失败都不报成功。其他平台无此方法时保持既有流程。
- 目录为明确填写的 model_ids，不是自动发现，不伪造模型窗口或能力。导入只验证首个型号，其他型号仍需实际调用确认。
- 三种客户端协议各覆盖流式/非流式、共享名称、来源与用量12/3、空分组拒绝及并发归零；原生工具参数大整数、图片编码、签名保留、错误状态和取消已覆盖。
- 导入测试覆盖先换取令牌、推理403不保存、保存失败可重试及最终保存新令牌。通用导入准备测试覆盖身份/地区改变、过期、错误/坏规范化，均不能调用验证或保存。

验证通过：/tmp/codearts-p5b-client-check.log（make check）、/tmp/codearts-p5b-client-race-final.log（定向竞态）、/tmp/codearts-p5b-browser/browser-results.json（中英文浏览器）。Linux/Windows 构建通过：/tmp/codearts-p5b-client-panel、/tmp/codearts-p5b-client-panel.exe。

未读取生产凭据、未部署、未推送或发布；真实云账户、完整型号授权与账单验证仍未执行。自动模型发现尚未实现，显式 model_ids 是当前支持范围；Vertex Claude/API Key Express/ADC 仍未支持。
