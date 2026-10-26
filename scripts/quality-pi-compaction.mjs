// Real Pi AgentSession automatic threshold compaction; synthetic history only.
import fs from 'node:fs';import path from 'node:path';import {pathToFileURL} from 'node:url';import {execFileSync} from 'node:child_process';
const withAdapter=process.argv.includes('--adapter');
const cfg=JSON.parse(fs.readFileSync(process.argv[2],'utf8')),output=process.argv[3];process.env.CODEARTS_API_KEY=cfg.key;process.env.PI_OFFLINE='1';process.env.PI_TELEMETRY='0';
const npmRoot=execFileSync('npm',['root','-g'],{encoding:'utf8'}).trim();const core=path.join(npmRoot,'@earendil-works/pi-coding-agent/dist/core');
const load=async f=>import(pathToFileURL(path.join(core,f)).href);
const {estimateProjectedContextTokens}=await load('compaction/compaction.js');
const {createAgentSession}=await load('sdk.js'),{SessionManager}=await load('session-manager.js'),{SettingsManager}=await load('settings-manager.js'),{ModelRuntime}=await load('model-runtime.js'),{DefaultResourceLoader}=await load('resource-loader.js');
const dir=path.join(path.dirname(path.resolve(process.argv[2])),'pi-compaction');fs.mkdirSync(dir,{recursive:true,mode:0o700});
const modelID='deepseek-v4.1-flash';fs.writeFileSync(path.join(dir,'models.json'),JSON.stringify({providers:{codearts:{baseUrl:cfg.base+'/v1',api:'openai-completions',apiKey:'${CODEARTS_API_KEY}',authHeader:true,compat:{maxTokensField:'max_tokens',supportsDeveloperRole:false,supportsStore:false},models:[{id:modelID,name:modelID,input:['text'],contextWindow:65536,maxTokens:8192}]}}}),{mode:0o600});
const runtime=await ModelRuntime.create({authPath:path.join(dir,'auth.json'),modelsPath:path.join(dir,'models.json'),allowModelNetwork:false,refreshOnCreate:false});const model=runtime.getModel('codearts',modelID);if(!model)throw Error('Pi custom model unavailable');
const adapter=withAdapter ? (await import(pathToFileURL(path.resolve('scripts/pi/codearts-compaction.ts')).href)).default : undefined;
const manager=SessionManager.inMemory(dir),settings=SettingsManager.inMemory({compaction:{enabled:true,reserveTokens:16384,keepRecentTokens:12000},retry:{enabled:false,maxRetries:0}});
const loader=new DefaultResourceLoader({cwd:dir,agentDir:dir,settingsManager:settings,extensionFactories:adapter?[adapter]:[],noExtensions:true,disabledBuiltinExtensions:['mcp'],noSkills:true,noPromptTemplates:true,noThemes:true,noContextFiles:true,systemPrompt:'You are a coding assistant testing a synthetic project. Preserve the project invariants in checkpoints. Answer each new user request briefly.'});await loader.reload();
const {session}=await createAgentSession({cwd:dir,agentDir:dir,modelRuntime:runtime,model,thinkingLevel:'off',tools:[],resourceLoader:loader,sessionManager:manager,settingsManager:settings});
let starts=0,ends=0;const failures=[];session.subscribe(e=>{if(e.type==='compaction_start')starts++;if(e.type==='compaction_end'){ends++;if(e.errorMessage||e.aborted)failures.push(e.errorMessage||'compaction aborted');}if(e.type==='session_compact_failed')failures.push('compaction failure');});
const burst=process.argv.includes('--burst');
const rounds=[],invariant='INVARIANT_QUALITY_LEDGER_USD';
try{
 for(let round=1;round<=3;round++){
  // Imported synthetic historical messages have no provider usage. Pi estimates
  // their size and naturally checks its threshold through session.prompt().
  const logline='fixture-build: request conversion preserved message roles, validated JSON, rejected invalid payload, recorded measured usage and released account lease. ';
  const block=(logline.repeat(58));
  let importedTurns=0;
  for(let n=0;n<(burst?26:40) && (burst || estimateProjectedContextTokens(manager.buildSessionProjection(),manager.getBranch()).tokens<49300);n++){
   importedTurns++;
   manager.appendMessage({role:'user',content:`Synthetic project QUALITY_SENTINEL. Permanent constraint ${invariant}: all prices use USD; never put credentials in logs. Completed milestone MILESTONE_${round}. Keep all MILESTONE_n checkpoint identifiers verbatim. This is duplicate build output to deduplicate in checkpoints, not text to quote. Task ${n}.\n`+block,timestamp:Date.now()});
   manager.appendMessage({role:'assistant',content:[{type:'text',text:`Checked task ${n}. Preserve ${invariant} and completed milestone MILESTONE_${round}.`}],api:'openai-completions',provider:'codearts',model:modelID,usage:{input:0,output:0,cacheRead:0,cacheWrite:0,totalTokens:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},stopReason:'stop',timestamp:Date.now()});
  }
  const estimatedBeforePrompt=estimateProjectedContextTokens(manager.buildSessionProjection(),manager.getBranch()).tokens;
  session.agent.state.messages = manager.buildSessionContext().messages;
  const timer=setTimeout(()=>session.abort(),240000);
  try{await session.prompt(`Continue the same project. Reply with exactly ROUND_${round}_OK. Remember ${invariant} and milestones.`);}finally{clearTimeout(timer)}
  const checkpoints=manager.getBranch().filter(e=>e.type==='compaction'),last=checkpoints.at(-1);const messages=manager.buildSessionContext().messages;
  const answer=messages.filter(m=>m.role==='assistant').at(-1);const text=(answer?.content||[]).filter(p=>p.type==='text').map(p=>p.text).join('');
  const passed=!!last&&checkpoints.length>=round&&last.summary.includes(invariant)&&Array.from({length:round},(_,i)=>`MILESTONE_${i+1}`).every(x=>last.summary.includes(x))&&text.includes(`ROUND_${round}_OK`)&&answer.stopReason==='stop'&&!failures.length;
  rounds.push({round,passed,imported_turns:importedTurns,estimated_tokens_before_prompt:estimatedBeforePrompt,automatic_starts:starts,automatic_ends:ends,checkpoint_count:checkpoints.length,summary_characters:last?.summary.length||0,recovery:last?.details?.codeartsRecovery,invariant_preserved:!!last?.summary.includes(invariant),all_milestones_preserved:Array.from({length:round},(_,i)=>`MILESTONE_${i+1}`).every(x=>last?.summary.includes(x)),answer_complete:answer?.stopReason==='stop',usage:last?.usage});
  fs.writeFileSync(output,JSON.stringify({kind:'real Pi AgentSession public prompt, automatic threshold',adapter:withAdapter,rounds,failures},null,2)+'\n');console.log('round '+round+': '+passed);
  if(!passed)throw Error('Automatic compaction round failed');
 }
} catch(e){fs.writeFileSync(output,JSON.stringify({kind:'real Pi AgentSession public prompt, automatic threshold',adapter:withAdapter,rounds,failures,error:String(e.message).replaceAll(cfg.key,'[REDACTED]')},null,2)+'\n');process.exitCode=1;}finally{await session.dispose();}
