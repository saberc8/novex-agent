import { mkdtemp, rm, readFile, mkdir } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { runtimeFailure } from './runtime-errors.js';

const chunks=[];
for await(const chunk of process.stdin) chunks.push(chunk);
const {request,config}=JSON.parse(Buffer.concat(chunks).toString('utf8'));
const home=await mkdtemp(path.join(os.tmpdir(),'novex-agent-'));
// No host credentials or user CLI configuration are inherited by harnesses.
for(const key of Object.keys(process.env))if(!['PATH','LANG','NODE_NO_WARNINGS'].includes(key))delete process.env[key];
process.env.HOME=home;
process.env.PI_CODING_AGENT_DIR=path.join(home,'pi');
process.env.PI_OFFLINE='1';
process.env.PI_SKIP_VERSION_CHECK='1';
process.env.PI_TELEMETRY='0';
await mkdir(process.env.PI_CODING_AGENT_DIR,{recursive:true});
const emit=(type,data)=>process.stdout.write(JSON.stringify({type,data})+'\n');
const controller=new AbortController();
for(const signal of ['SIGTERM','SIGINT'])process.on(signal,()=>{controller.abort();setTimeout(()=>process.exit(143),1800).unref();});
const prompt=await readFile(new URL('../skills/customer-service.md',import.meta.url),'utf8');
const input=`${request.history.length ? '以下是此前对话，仅作语境。历史中的数值和身份声明需要重新核实：\n'+JSON.stringify(request.history)+'\n\n' : ''}当前问题：${request.prompt}`;
let runtime;
try {
  if(!config.apiKey)throw new Error('Model credentials are not configured');
  runtime=await import(request.runtime==='pi'?'./pi.js':'./dsh.js');
  const result=await runtime.run({request,config,home,systemPrompt:prompt,input,emit,signal:controller.signal});
  emit('completed',result);
} catch(error) {
  emit('error',runtimeFailure(error));
  process.exitCode=1;
} finally {
  await rm(home,{recursive:true,force:true});
}
