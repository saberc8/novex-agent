import path from 'node:path';
import { realpath } from 'node:fs/promises';
export const name='novex-tool-guard';
export const inject=['tools'];
export function apply(ctx,config) {
  let calls=0;
  ctx.on('tools/execute',async(exec,next)=>{
    if(++calls>config.maxToolCalls)throw new Error('Tool call budget exceeded');
    const allowed=['read','glob','grep'];
    if(!allowed.includes(exec.name)&&!exec.name.startsWith('mcp__business__'))throw new Error('Tool denied by Novex policy');
    if(allowed.includes(exec.name)) {
      const args=exec.arguments||{};
      const p=await realpath(path.resolve(config.workspacePath,args.file_path||args.path||'.'));
      const root=await realpath(config.workspacePath);
      if(p!==root&&!p.startsWith(root+path.sep))throw new Error('Access denied: path is outside the authorized knowledge workspace');
    }
    return next();
  });
}
