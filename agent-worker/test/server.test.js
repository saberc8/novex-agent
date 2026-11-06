import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm } from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';

test('worker rejects unauthenticated requests, unapproved MCP origins, and workspace escape before starting an agent',async()=>{
  const dir=await mkdtemp(path.join(os.tmpdir(),'novex-worker-http-'));
  const allowed=path.join(dir,'views');await mkdir(allowed);
  const child=spawn(process.execPath,['src/server.js'],{stdio:['ignore','pipe','pipe'],env:{PATH:process.env.PATH,NOVEX_RUNNER_SECRET:'test-control-secret',NOVEX_RUNNER_PORT:'0',NOVEX_WORKSPACE_ROOT:allowed,NOVEX_EXECUTION_MODE:'subprocess'}});
  try {
    const port=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',code=>reject(new Error(`Worker exited ${code}`)));child.stdout.on('data',b=>{const match=String(b).match(/listening on (\d+)/);if(match)resolve(Number(match[1]));});});
    const base=`http://127.0.0.1:${port}`;
    assert.equal((await fetch(`${base}/run`,{method:'POST'})).status,401);
    const health=await (await fetch(`${base}/health`)).json();assert.equal(health.osSandbox,false);assert.equal(health.runtimes.pi.available,true);
    const payload={runId:'test-run',runtime:'pi',prompt:'question',history:[],workspacePath:dir,mcpUrl:'http://attacker.invalid/mcp',mcpToken:'cap'};
    const headers={'content-type':'application/json',Authorization:'Bearer test-control-secret'};
    let response=await fetch(`${base}/run`,{method:'POST',headers,body:JSON.stringify(payload)});assert.equal(response.status,400);assert.match((await response.json()).error,/origin/);
    payload.mcpUrl='http://127.0.0.1:8310/api/novex/mcp';
    response=await fetch(`${base}/run`,{method:'POST',headers,body:JSON.stringify(payload)});assert.equal(response.status,400);assert.match((await response.json()).error,/workspace root/);
    assert.equal((await (await fetch(`${base}/health`)).json()).activeRuns,0);
  }finally{child.kill('SIGTERM');await new Promise(r=>child.once('exit',r));await rm(dir,{recursive:true,force:true});}
});
