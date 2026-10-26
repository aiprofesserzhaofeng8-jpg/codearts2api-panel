#!/usr/bin/env python3
"""Publish a small reviewed acceptance summary, excluding private/raw runtime files."""
import argparse,json,pathlib
from quality_common import ROOT,save_json,utc
p=argparse.ArgumentParser(description=__doc__);p.add_argument('run_dir',type=pathlib.Path);a=p.parse_args();run=a.run_dir.resolve();data=json.loads((run/'results.json').read_text());cases={k:{'status':v['status'],'detail':v['detail']} for k,v in data['cases'].items()}
soak_file=run/'soak.json';soak=json.loads(soak_file.read_text()) if soak_file.exists() else {}
if soak:
 cases['R03']={'status':soak['status'],'detail':soak['summary']};cases['R04']={'status':'PASS' if soak.get('refresh_observed') else 'PENDING','detail':'已观察真实凭据续期后生成成功' if soak.get('refresh_observed') else '等待真实续期证据'}
environment=json.loads((run/'environment.json').read_text());allowed_env=('platform','go','node','pi_version','codex_version','claude_version','source_sha256','binary_sha256','remediation_source_sha256')
summary={'exported_at':utc(),'run_id':run.name,'cases':cases,'environment':{k:environment[k] for k in allowed_env if k in environment},'go_summary':json.loads((run/'go-summary.json').read_text()),'controlled_performance':json.loads((run/'performance.json').read_text()),'soak':{k:soak[k] for k in ('status','elapsed_hours','health_success_rate','restart_count','refresh_observed') if k in soak},'scope':'Linux, single process, one administrator. Synthetic inputs for live upstream checks. Not an ISO certification.','private_data_included':False}
target=ROOT/'docs/acceptance/evidence/quality-snapshot.json';save_json(target,summary)
counts={s:sum(v['status']==s for v in cases.values()) for s in ('PASS','FAIL','BLOCKED','PENDING')};conclusion='验收范围内全部通过' if counts['FAIL']==counts['BLOCKED']==counts['PENDING']==0 else '尚未通过完整验收'
lines=['# 质量验收结果快照','',f'导出时间：{summary["exported_at"]}。本次验收：`{run.name}`。', '',f'**{conclusion}。** '+ '；'.join(f'{s} {n}' for s,n in counts.items())+'。','', '这是提交到源码仓库时的结果快照。持续观察产生的原始日志、会话、账号状态、密钥和机器私有路径不随源码发布。后续结果可能变化，不能把此快照视为实时状态。','', '验收步骤与通过标准见 [QUALITY-PLAN.md](QUALITY-PLAN.md)，脱敏后的结构化摘要见 [quality-snapshot.json](evidence/quality-snapshot.json)。','', '| 编号 | 结果 | 说明 |','| --- | --- | --- |']
for ident,c in sorted(cases.items()):lines.append('| '+ident+' | '+c['status']+' | '+c['detail'].replace('|','/').replace('\n',' ')+' |')
lines += ['', '## 已知边界', '', '- Codex 原生工具沙箱若被 Ubuntu 用户命名空间/AppArmor 限制阻断，需要本机管理员按 [修复说明](REMEDIATION-2026-10-26.md) 加载官方 profile。', '- Pi 分段摘要恢复已完成正常和突发历史各三轮真实验证；运行中的 Pi 需要重新载入扩展。分段数量、调用次数和输入大小均有限制。', '- 已测试的指定函数形式 tool_choice 被华为上游拒绝，自动选择工具的三协议闭环通过。', '- Token / USD 是结算后的限额；USD 是 API 参考估价，不能当作华为积分账单。', '- 未验证的操作系统、部署方式和客户端能力不在此快照的通过声明之内。','']
(ROOT/'docs/acceptance/QUALITY-SNAPSHOT.md').write_text('\n'.join(lines));print('Exported reviewed quality snapshot without raw runtime artifacts.')
