#!/usr/bin/env python3
import argparse,json,pathlib,fcntl
from quality_common import ROOT,utc
p=argparse.ArgumentParser();p.add_argument('run_dir',type=pathlib.Path);args=p.parse_args();run=args.run_dir.resolve();lock=(run/'render.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX);report=json.loads((run/'results.json').read_text());cases=dict(report['cases'])
soak=run/'soak.json'
if soak.exists():
 s=json.loads(soak.read_text());cases['R03']={'status':s['status'],'detail':s['summary'],'evidence':'soak.json'};cases['R04']={'status':'PASS' if s.get('refresh_observed') else ('BLOCKED' if s['status']=='FAIL' else 'PENDING'),'detail':'已观察真实续期后生成成功' if s.get('refresh_observed') else '等待生成跨越真实凭据有效期；不将模拟续期计为真实通过','evidence':'soak.json'}
counts={status:sum(c['status']==status for c in cases.values()) for status in ['PASS','FAIL','PENDING','BLOCKED','NA']};overall='完整验收通过' if all(v['status'] in ('PASS','NA') for v in cases.values()) else '尚未通过完整验收'
lines=['# 软件质量验收报告','',f'更新：{utc()}。本次记录：`{run.relative_to(ROOT)}`。', '',f'**结论：{overall}。** '+ '；'.join(f'{k} {v}' for k,v in counts.items())+'。','', '本轮证据对应方案 [QUALITY-PLAN.md](QUALITY-PLAN.md)。模拟、真实上游和客户端证据分别记录；24 小时未到不会提前判定通过。','', '| 编号 | 状态 | 实测结果 | 证据 |','| --- | --- | --- | --- |']
for ident,c in sorted(cases.items()):
 detail=str(c['detail']).replace('|','/').replace('\n',' ');ev=c.get('evidence','');link=f'[{ev}](../../{(run/ev).relative_to(ROOT)})' if ev else '—';lines.append(f'| {ident} | {c["status"]} | {detail} | {link} |')
findings_file=run/'findings.json'
if findings_file.exists():
 findings=json.loads(findings_file.read_text());lines += ['', '## 发现的问题', '']
 for item in findings.get('open',[]):lines.append('- **'+item['status']+'：**'+item['issue']+'。')
 for item in findings.get('resolved',[]):lines.append('- **已修正：**'+item['issue']+'；'+item['resolution']+'。')
 lines.append(f'- 详细记录：[findings.json](../../{(run/"findings.json").relative_to(ROOT)})。')
lines += ['', '## 环境与边界', '', '- 当前运行环境：Linux，本机单进程。其他操作系统和多实例未在本轮验证。', '- 真实上游验证消耗账号额度；报告中的 USD 为价格表估价，不等于华为积分账单。', '- 原有账号和密钥不用于删除/重置实验；主验收密钥和观察密钥的清理状态分别记录。', '- 指纹与版本见 environment.json；每项原始脱敏证据位于本次记录目录。', '- 长时观察文件会持续更新，完成后本报告重新计算结论。', '']
text='\n'.join(lines);(run/'REPORT.md').write_text(text);(ROOT/'docs/acceptance/QUALITY-REPORT.md').write_text(text)
print(overall+' '+json.dumps(counts,ensure_ascii=False))
