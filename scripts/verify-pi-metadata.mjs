// Offline verification of the installed Pi client. No configuration or secrets read.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readdir, readFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
const directory = process.argv[2];
if (!directory) throw new Error('Usage: node scripts/verify-pi-metadata.mjs <Pi bundle/chunks directory>');
const root = resolve(directory), files = await readdir(root);
const load = async prefix => {
 const name = files.find(n => n.startsWith(prefix) && n.endsWith('.js'));
 assert.ok(name, `Missing ${prefix} module`);
 return { name, sha256: createHash('sha256').update(await readFile(join(root, name))).digest('hex'), module: await import(pathToFileURL(join(root, name)).href) };
};
const details = [{type:'reasoning.encrypted',id:'opaque-fixture',data:'OPAQUE_FIXTURE',format:'fixture'}];
const signature = 'GOOGLE_SIGNATURE_FIXTURE';
const cost = {input:0,output:0,cacheRead:0,cacheWrite:0};
const tools = [{name:'lookup',description:'Fixture tool',parameters:{type:'object',properties:{},required:[]}}];
const user = {role:'user',content:'Call lookup.',timestamp:1};
const sse = (events, anthropic=false) => events.map(e => (anthropic ? `event: ${e.type}\n` : '') + `data: ${JSON.stringify(e)}\n\n`).join('') + (anthropic?'':'data: [DONE]\n\n');
const chatEvents = [
 {id:'fixture',choices:[{index:0,delta:{role:'assistant',reasoning_details:details,tool_calls:[{index:0,id:'call_fixture',type:'function',function:{name:'lookup',arguments:'{}'},extra_content:{google:{thought_signature:signature}}}]},finish_reason:null}]},
 {id:'fixture',choices:[{index:0,delta:{},finish_reason:'tool_calls'}],usage:{prompt_tokens:2,completion_tokens:3}}
];
const anthropicEvents = [
 {type:'message_start',message:{id:'fixture',role:'assistant',model:'fixture',content:[],usage:{input_tokens:2,output_tokens:0}}},
 {type:'content_block_start',index:0,content_block:{type:'thinking',thinking:'',signature:'NATIVE_SIGNATURE',reasoning_details:details}},
 {type:'content_block_delta',index:0,delta:{type:'thinking_delta',thinking:'Fixture thought',reasoning_details:details}},
 {type:'content_block_stop',index:0},
 {type:'content_block_start',index:1,content_block:{type:'tool_use',id:'call_fixture',name:'lookup',input:{}}},
 {type:'content_block_delta',index:1,delta:{type:'input_json_delta',partial_json:'{}'}},
 {type:'content_block_stop',index:1},
 {type:'message_delta',delta:{stop_reason:'tool_use'},usage:{output_tokens:3}},
 {type:'message_stop'}
];
for (const [prefix, api, events, anthropic] of [
 ['openai-completions-', 'openai-completions', chatEvents, false],
 ['anthropic-messages-', 'anthropic-messages', anthropicEvents, true]
]) {
 const loaded = await load(prefix), requests=[];
 const model = {id:'fixture',name:'fixture',provider:'codearts-fixture',api,baseUrl:'https://fixture.invalid/v1',input:['text'],reasoning:true,contextWindow:32768,maxTokens:1024,cost};
 const fetch = async (_url, init) => {
  requests.push(JSON.parse(init.body));
  return new Response(sse(events,anthropic),{status:200,headers:{'content-type':'text/event-stream'}});
 };
 const options = {apiKey:'synthetic-fixture',fetch,maxTokens:128,maxRetries:0};
 const first = await loaded.module.stream(model,{messages:[user],tools},options).result();
 assert.notEqual(first.stopReason,'error',first.errorMessage);
 const tool = first.content.find(c=>c.type==='toolCall');assert.ok(tool,'Tool call not decoded');
 const next = {role:'toolResult',toolCallId:tool.id,toolName:tool.name,content:[{type:'text',text:'fixture result'}],isError:false,timestamp:2};
 const second = await loaded.module.stream(model,{messages:[user,first,next],tools},options).result();
 assert.notEqual(second.stopReason,'error',second.errorMessage);assert.equal(requests.length,2);
 const replay = JSON.stringify(requests[1]);
 const assistant = requests[1].messages.find(m=>m.role==='assistant');
 assert.ok(assistant, 'Assistant history not replayed');
 const replayMetadata = JSON.stringify(assistant);
 console.log(JSON.stringify({client_module:loaded.name,client_module_sha256:loaded.sha256,api,network:false,credentials:false,two_rounds:true,tool_result_replayed:replay.includes('fixture result'),cline_reasoning_details_preserved:replayMetadata.includes('OPAQUE_FIXTURE'),google_tool_signature_preserved:replayMetadata.includes(signature),native_anthropic_signature_preserved:replayMetadata.includes('NATIVE_SIGNATURE')}));
 assert.ok(replay.includes('fixture result'));
}
