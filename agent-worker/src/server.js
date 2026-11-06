import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';
import { randomUUID, timingSafeEqual } from 'node:crypto';
import { realpath, lstat, readdir } from 'node:fs/promises';
import { createInterface } from 'node:readline';
import { safeFailure } from './runtime-errors.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const active = new Map();
const secret = process.env.NOVEX_RUNNER_SECRET;
if (!secret) throw new Error('NOVEX_RUNNER_SECRET must be configured');
const mode = process.env.NOVEX_EXECUTION_MODE || 'subprocess';
const root = await realpath(process.env.NOVEX_WORKSPACE_ROOT || path.resolve(here, '../../backend/data/workspaces'));
const maxRuns = Number(process.env.NOVEX_MAX_CONCURRENT_RUNS || 4);
const timeout = Number(process.env.NOVEX_RUN_TIMEOUT_MS || 180000);
const json = (res, status, body) => { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); };
const authorized = req => { const a=Buffer.from(req.headers.authorization || ''), b=Buffer.from(`Bearer ${secret}`); return a.length===b.length && timingSafeEqual(a,b); };
async function safeWorkspace(input) {
  if (typeof input !== 'string' || !path.isAbsolute(input)) throw new Error('workspacePath must be absolute');
  const resolved = await realpath(input);
  if (!resolved.startsWith(root + path.sep)) throw new Error('workspacePath is outside configured workspace root');
  async function inspect(dir) { for (const entry of await readdir(dir,{withFileTypes:true})) { const p=path.join(dir,entry.name); const s=await lstat(p); if(s.isSymbolicLink()) throw new Error('Workspace symlinks are forbidden'); if(s.isDirectory()) await inspect(p); else if(!s.isFile()) throw new Error('Workspace must contain regular files only'); } }
  await inspect(resolved);
  return resolved;
}
async function body(req) {
  const chunks=[]; let bytes=0;
  for await(const chunk of req) { bytes+=chunk.length; if(bytes>512000) throw new Error('Request too large'); chunks.push(chunk); }
  return JSON.parse(Buffer.concat(chunks).toString('utf8'));
}
function stop(run) { if (!run || run.finished) return; void run.revoke?.(); run.process?.kill('SIGTERM'); const t=setTimeout(()=>{run.process?.kill('SIGKILL');if(run.containerName)spawn('docker',['rm','-f',run.containerName],{stdio:'ignore'}).unref();},2000); t.unref(); }
const server=http.createServer(async(req,res)=>{
  if(req.url==='/health' && req.method==='GET') return json(res,200,{ok:true,runtimes:{pi:{available:true,version:'1.5.0'},dsh:{available:true,version:'0.2.2-rc.1'}},executionMode:mode,osSandbox:mode==='docker',activeRuns:active.size});
  if(!authorized(req)) return json(res,401,{error:'Unauthorized'});
  const match=req.url?.match(/^\/runs\/([a-zA-Z0-9_-]+)\/cancel$/);
  if(match && req.method==='POST') { const run=active.get(match[1]); if(run) { run.cancelled=true; stop(run); } return json(res,200,{cancelled:Boolean(run)}); }
  if(req.url!=='/run' || req.method!=='POST') return json(res,404,{error:'Not found'});
  if(active.size>=maxRuns) return json(res,429,{error:'Worker capacity reached'});
  let data;
  try {
    data=await body(req);
    if(!/^[a-zA-Z0-9_-]{1,100}$/.test(data.runId || '') || !['pi','dsh'].includes(data.runtime)) throw new Error('Invalid runId or runtime');
    if(active.has(data.runId)) return json(res,409,{error:'Run already active'});
    if(typeof data.prompt!=='string'||!data.prompt.trim()||data.prompt.length>64000) throw new Error('Invalid prompt');
    if(!Array.isArray(data.history)||data.history.length>50||data.history.some(x=>!['user','assistant'].includes(x.role)||typeof x.content!=='string'||x.content.length>64000)) throw new Error('Invalid history');
    const mcp=new URL(data.mcpUrl);
    const configured=process.env.NOVEX_MCP_ALLOWED_ORIGIN || 'http://127.0.0.1:8310';
    if(mcp.origin!==configured) throw new Error('MCP origin is not allowed');
    if(typeof data.mcpToken!=='string'||!data.mcpToken) throw new Error('Missing MCP token');
    data.workspacePath=await safeWorkspace(data.workspacePath);
  } catch(error) { return json(res,400,{error:error.message}); }
  // Reserve capacity before the asynchronous capability request. Concurrent
  // submissions must not overwrite one run or exceed the configured limit.
  if(active.has(data.runId))return json(res,409,{error:'Run already active'});
  if(active.size>=maxRuns)return json(res,429,{error:'Worker capacity reached'});
  const run={process:null,finished:false,cancelled:false,started:Date.now()};
  active.set(data.runId,run);
  res.writeHead(200,{'content-type':'text/event-stream','cache-control':'no-cache','connection':'keep-alive','x-accel-buffering':'no'});
  const emit=(event,payload)=>{if(!res.destroyed)res.write(`event: ${event}\ndata: ${JSON.stringify(payload)}\n\n`);};
  emit('status',{message:`正在启动 ${data.runtime === 'pi' ? 'Pi' : 'DeepSeek Harness'}`});
  const proxyUrl=process.env.NOVEX_MODEL_PROXY_URL||'http://127.0.0.1:8330';
  let capability;
  try {
    const response=await fetch(`${proxyUrl}/internal/tokens`,{method:'POST',headers:{Authorization:`Bearer ${secret}`,'content-type':'application/json'},body:JSON.stringify({runId:data.runId,ttlSeconds:Math.ceil(timeout/1000)+30}),signal:AbortSignal.timeout(10000)});
    if(!response.ok)throw new Error('Model capability unavailable');
    capability=await response.json();
  }catch {active.delete(data.runId);emit('error',{message:'Model proxy is unavailable'});res.end();return;}
  const revoke=()=>fetch(`${proxyUrl}/internal/tokens/${encodeURIComponent(data.runId)}`,{method:'DELETE',headers:{Authorization:`Bearer ${secret}`},signal:AbortSignal.timeout(5000)}).catch(()=>{});
  if(run.cancelled||res.destroyed){await revoke();active.delete(data.runId);res.end();return;}
  const config={baseUrl:mode==='docker'?capability.baseUrl:capability.baseUrl.replace('host.docker.internal','127.0.0.1'),model:process.env.NOVEX_MODEL_NAME||'deepseek-v4.1-flash',apiKey:capability.token,maxTokens:Number(process.env.NOVEX_MAX_OUTPUT_TOKENS||4096),maxToolCalls:Number(process.env.NOVEX_MAX_TOOL_CALLS||20)};
  let cmd=process.execPath,args=[path.join(here,'execute.js')],containerName;
  if(mode==='docker') {
    containerName=`novex-run-${randomUUID()}`;
    cmd='docker'; args=['run','--rm','--name',containerName,'--init','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--pids-limit=128','--memory=1536m','--cpus=1','--user=1000:1000','--tmpfs','/tmp:rw,nosuid,nodev,size=256m,mode=1777','--mount',`type=bind,source=${data.workspacePath},target=/workspace,readonly`,'--add-host','host.docker.internal:host-gateway'];
    if(process.env.NOVEX_DOCKER_NETWORK) args.push('--network',process.env.NOVEX_DOCKER_NETWORK);
    args.push('-i',process.env.NOVEX_RUNNER_IMAGE||'novex-agent-runtime:local','node','/app/src/execute.js');
    data={...data,workspacePath:'/workspace'};
  }
  const child=spawn(cmd,args,{stdio:['pipe','pipe','pipe'],env:{PATH:process.env.PATH,HOME:process.env.HOME,TMPDIR:process.env.TMPDIR,LANG:'C.UTF-8',NODE_NO_WARNINGS:'1'}});
  Object.assign(run,{process:child,containerName,revoke});
  const deadline=setTimeout(()=>{run.timedOut=true;stop(run);},timeout);
  const pulse=setInterval(()=>{if(!res.destroyed)res.write(': heartbeat\n\n');},10000);
  let terminal=false;
  const finish=(event,payload)=>{if(terminal)return;terminal=true;if(event==='error'){payload=safeFailure(payload);console.error(JSON.stringify({event:'agent_failure',runId:data.runId,runtime:data.runtime,elapsedMs:Date.now()-run.started,...payload}));}emit(event,payload);res.end();};
  const lines=createInterface({input:child.stdout});
  lines.on('line',line=>{try {const e=JSON.parse(line); if(e.type==='completed'||e.type==='error')finish(e.type,e.data); else if(e.type==='status')emit(e.type,e.data);} catch { /* Never relay uncontrolled runtime stdout. */ }});
  child.stderr.on('data',()=>{}); // Provider errors can contain credentials; child emits sanitized failures.
  child.once('error',()=>finish('error',{code:'RUNTIME_START_ERROR'}));
  child.once('close',()=>{run.finished=true;active.delete(data.runId);clearTimeout(deadline);clearInterval(pulse);void revoke();finish('error',{code:run.cancelled?'CANCELLED':run.timedOut?'TIMEOUT':'RUNTIME_EXIT'});});
  res.on('close',()=>{if(!terminal){run.cancelled=true;stop(run);}});
  child.stdin.end(JSON.stringify({request:data,config}));
});
server.listen(Number(process.env.NOVEX_RUNNER_PORT||8320),process.env.NOVEX_RUNNER_HOST||'127.0.0.1',()=>console.log(`Novex agent worker listening on ${server.address().port}; isolation=${mode}`));
for(const signal of ['SIGTERM','SIGINT'])process.on(signal,()=>{server.close();for(const run of active.values())stop(run);setTimeout(()=>process.exit(0),2500).unref();});
