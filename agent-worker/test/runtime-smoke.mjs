// Runs REAL harness loops against the configured model proxy and an MCP fixture.
import http from 'node:http';
import { mkdtemp, writeFile, rm, chmod } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
const root=await mkdtemp(path.join(os.tmpdir(),'novex-runtime-smoke-'));
await chmod(root,0o755);
await writeFile(path.join(root,'INDEX.md'),'# 授权知识库\n休假政策详见 policy.md\n');
await writeFile(path.join(root,'policy.md'),'# 休假政策\n入职满一年可享受 7 天年假。\n');
const seen=[];
const server=http.createServer(async(req,res)=>{
  if(req.method==='GET'){res.writeHead(405);res.end();return;}
  const parts=[];for await(const b of req)parts.push(b);
  const msg=JSON.parse(Buffer.concat(parts).toString());
  if(!('id'in msg)){res.writeHead(202);res.end();return;}
  let result;
  if(msg.method==='initialize')result={protocolVersion:'2025-03-26',capabilities:{tools:{}},serverInfo:{name:'novex-smoke',version:'1'}};
  else if(msg.method==='tools/list')result={tools:[{name:'list_resources',description:'列出当前用户可查询的业务资源',inputSchema:{type:'object',properties:{}}},{name:'query_resource',description:'读取当前用户自己的工资。必须先list_resources。返回金额为虚构测试数据。',inputSchema:{type:'object',properties:{resourceId:{type:'string'}},required:['resourceId']}}]};
  else if(msg.method==='tools/call'){seen.push(msg.params.name);result={content:[{type:'text',text:JSON.stringify(msg.params.name==='list_resources'?[{id:'salary',name:'我的工资',fields:['amount','month']}]:{amount:12345,month:'2026-10',currency:'CNY'})}]};}
  else result={};
  res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({jsonrpc:'2.0',id:msg.id,result}));
});
await new Promise(r=>server.listen(0,'0.0.0.0',r));
const modelProxy=process.env.NOVEX_MODEL_PROXY_URL||'http://127.0.0.1:8330';
const headers={Authorization:`Bearer ${process.env.NOVEX_RUNNER_SECRET}`,'content-type':'application/json'};
const docker=process.env.NOVEX_SMOKE_DOCKER==='1';
try {
  for(const runtime of process.argv.slice(2).length?process.argv.slice(2):['pi','dsh']) {
    seen.length=0;
    const runId=`smoke-${runtime}-${randomUUID()}`;
    const capResponse=await fetch(`${modelProxy}/internal/tokens`,{method:'POST',headers,body:JSON.stringify({runId,ttlSeconds:240})});
    if(!capResponse.ok)throw new Error(`Proxy capability status ${capResponse.status}`);
    const cap=await capResponse.json();
    const payload={request:{runId,runtime,prompt:'请查询我的2026年10月工资，并查阅本地知识库告诉我入职满一年有几天年假。必须实际调用MCP和本地文件工具，回答包含金额、天数和文件引用。',history:[],workspacePath:root,mcpUrl:`http://127.0.0.1:${server.address().port}/mcp`,mcpToken:'smoke-scoped-token'},config:{baseUrl:cap.baseUrl.replace('host.docker.internal','127.0.0.1'),model:process.env.NOVEX_MODEL_NAME||'deepseek-v4.1-flash',apiKey:cap.token,maxTokens:2048,maxToolCalls:12}};
    if(docker){payload.request.workspacePath='/workspace';payload.request.mcpUrl=payload.request.mcpUrl.replace('127.0.0.1','host.docker.internal');payload.config.baseUrl=cap.baseUrl;}
    const child=spawn(docker?'docker':process.execPath,docker?['run','--rm','--init','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--pids-limit=128','--memory=1536m','--user=1000:1000','--tmpfs','/tmp:rw,nosuid,nodev,size=256m,mode=1777','--mount',`type=bind,source=${root},target=/workspace,readonly`,'--add-host','host.docker.internal:host-gateway','-i',process.env.NOVEX_RUNNER_IMAGE||'novex-agent-runtime:local','node','/app/src/execute.js']:['src/execute.js'],{stdio:['pipe','pipe','pipe'],env:{PATH:process.env.PATH,LANG:'C.UTF-8'}});
    let output='',stderr='';child.stdout.on('data',b=>{output+=b;});child.stderr.on('data',b=>stderr+=b);
    child.stdin.end(JSON.stringify(payload));
    const timeout=setTimeout(()=>child.kill('SIGKILL'),180000);
    const code=await new Promise(r=>child.once('close',r));clearTimeout(timeout);
    await fetch(`${modelProxy}/internal/tokens/${runId}`,{method:'DELETE',headers});
    const events=output.trim().split('\n').filter(Boolean).map(x=>{try{return JSON.parse(x);}catch{return null;}}).filter(Boolean);
    const final=events.find(e=>e.type==='completed');
    console.log(JSON.stringify({runtime,exitCode:code,toolCalls:seen,events,stderr:stderr.slice(-3500)}));
    if(code!==0||!final||!seen.includes('query_resource')||(!final.data.text.includes('12345')&&!final.data.text.includes('12,345'))||!final.data.text.includes('7'))process.exitCode=1;
  }
}finally{server.close();await rm(root,{recursive:true,force:true});}
