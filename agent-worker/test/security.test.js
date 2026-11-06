import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, symlink, rm } from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { assertInside } from '../src/pi.js';
import { apply } from '../src/dsh-guard.js';

test('Pi native file wrapper permits authorized files and denies traversal/symlink escape',async()=>{
  const dir=await mkdtemp(path.join(os.tmpdir(),'novex-access-'));
  try {
    const root=path.join(dir,'allowed');await mkdir(root);await writeFile(path.join(root,'policy.md'),'policy');
    const secret=path.join(dir,'secret.md');await writeFile(secret,'private');await symlink(secret,path.join(root,'escape.md'));
    assert.match(await assertInside(root,'policy.md'),/policy\.md$/);
    await assert.rejects(()=>assertInside(root,'../secret.md'),/Access denied/);
    await assert.rejects(()=>assertInside(root,'escape.md'),/Access denied/);
  }finally{await rm(dir,{recursive:true,force:true});}
});
test('DSH plugin rejects shell, foreign MCP namespaces, and escaped file reads before executor',async()=>{
  const dir=await mkdtemp(path.join(os.tmpdir(),'novex-dsh-access-'));
  try {
    const root=path.join(dir,'allowed');await mkdir(root);await writeFile(path.join(root,'policy.md'),'policy');await writeFile(path.join(dir,'secret.md'),'private');
    let handler;apply({on:(_event,fn)=>{handler=fn;}},{workspacePath:root,maxToolCalls:20});
    let executed=0;const next=async()=>{executed++;return {isError:false};};
    await handler({name:'read',arguments:{file_path:'policy.md'}},next);
    await handler({name:'mcp__business__list_resources',arguments:{}},next);
    await assert.rejects(()=>handler({name:'bash',arguments:{command:'env'}},next),/denied/);
    await assert.rejects(()=>handler({name:'mcp__other__query',arguments:{}},next),/denied/);
    await assert.rejects(()=>handler({name:'read',arguments:{file_path:'../secret.md'}},next),/Access denied/);
    assert.equal(executed,2);
  }finally{await rm(dir,{recursive:true,force:true});}
});
