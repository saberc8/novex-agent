import path from 'node:path';
import { writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { DeepSeekHarness } from '@deepseek-ai/dsh-sdk-client';

export async function run({request,config,home,systemPrompt,input,emit,signal}) {
  const patch=[
    ...['llm-deepseek','persistent-bash','persistent-pwsh','terminal-bash','terminal-pwsh','tool-working-directory'].map(id=>({id,disabled:true})),
    {id:'tool-fs',config:{read:true,readImage:false,write:false,edit:false,applyPatch:false,readMaxBytes:48000}},
    {id:'sandbox-policy',config:{mode:'read-only',workspaceRoot:request.workspacePath}},
    {id:'system-prompt',config:{includeHarnessIdentity:false,includeRuntimeContext:false,personaPrefix:systemPrompt}},
    {insert:[
      {id:'novex-llm',name:'@deepseek-ai/dsh-llm-pi-ai',config:{providers:{novex:{apiKeyEnv:'NOVEX_RUN_MODEL_TOKEN',api:'openai-completions',baseURL:config.baseUrl,defaultContextWindow:128000,defaultMaxTokens:config.maxTokens,models:[{id:config.model,contextWindow:128000,maxTokens:config.maxTokens,reasoningEfforts:false}],compat:{supportsDeveloperRole:false,supportsStore:false,maxTokensField:'max_tokens',thinkingFormat:'deepseek'},retryPolicy:{mode:'normal',maxRetries:0}}}}},
      {id:'novex-search',name:'@deepseek-ai/dsh-tool-fs-search',config:{sampleOverCapGlobResults:false,globMaxResults:100,grepMaxMatches:60}},
      {id:'novex-guard',name:fileURLToPath(new URL('./dsh-guard.js',import.meta.url)),config:{workspacePath:request.workspacePath,maxToolCalls:config.maxToolCalls}},
      {id:'novex-mcp',name:'@deepseek-ai/dsh-mcp-client',config:{transport:'streamable-http',serverName:'business',url:request.mcpUrl,headers:{Authorization:`Bearer ${request.mcpToken}`},toolCallTimeoutMs:30000,failOnStartupError:true}}
    ]}
  ];
  const patchPath=path.join(home,'novex.patch.json');
  await writeFile(patchPath,JSON.stringify(patch),{mode:0o600});
  // Load the pinned native addon from the immutable image; do not copy a shared
  // library into Docker's noexec temporary filesystem.
  const harness=new DeepSeekHarness({profile:'sdk-minimal',patches:[patchPath],dshHome:path.join(home,'dsh'),cwd:request.workspacePath,processCwd:request.workspacePath,provider:'novex',model:config.model,maxTokens:config.maxTokens,initializeTimeoutMs:30000,requestTimeoutMs:150000,env:{PATH:process.env.PATH,HOME:home,LANG:'C.UTF-8',NOVEX_RUN_MODEL_TOKEN:config.apiKey,NODE_NO_WARNINGS:'1',NARB_DISABLE_NATIVE_CACHE:'1'}});
  let toolCalls=0;
  signal?.addEventListener('abort',()=>void harness.close(),{once:true});
  try {
    await harness.start();
    emit('status',{message:'DeepSeek Harness 已连接授权知识库与业务 MCP'});
    const result=await harness.run(input,{onNotification:n=>{
      const event=n.params?.event;
      if(event?.type==='tool/call'){toolCalls++;emit('status',{message:'DeepSeek Harness 正在读取授权数据'});}
    }});
    if(!result.finalResponse?.trim()||!['stop','end_turn','completed'].includes(result.finishReason))throw Object.assign(new Error(`DeepSeek Harness did not complete: ${result.finishReason||'no final response'}${result.finishReason==='error'?': '+result.finalResponse:''}`),{stopReason:result.finishReason});
    return {text:result.finalResponse,usage:{runtime:'dsh',version:'0.2.2-rc.1',toolCalls,finishReason:result.finishReason}};
  }catch(error){if(error&&typeof error==='object')error.toolCalls=toolCalls;throw error;
  }finally{await harness.close();}
}
