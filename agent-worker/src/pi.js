import path from 'node:path';
import { realpath } from 'node:fs/promises';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { createAgentSession, createExtensionRuntime, ModelRuntime, SessionManager, SettingsManager, createReadTool, createGrepTool, createFindTool, createLsTool } from '@earendil-works/pi-coding-agent';

export async function assertInside(root, input='.') {
  root=await realpath(root);
  const resolved=await realpath(path.resolve(root,input));
  if(resolved!==root&&!resolved.startsWith(root+path.sep))throw new Error('Access denied: path is outside the authorized knowledge workspace');
  return resolved;
}
export async function run({request,config,home,systemPrompt,input,emit,signal}) {
  const mcp=new Client({name:'novex-pi',version:'0.1.0'});
  const transport=new StreamableHTTPClientTransport(new URL(request.mcpUrl),{requestInit:{headers:{Authorization:`Bearer ${request.mcpToken}`}}});
  let session,toolCalls=0;
  try {
    await mcp.connect(transport);
    const listed=await mcp.listTools();
    const budget=()=>{if(++toolCalls>config.maxToolCalls){void session?.abort();throw new Error('Tool call budget exceeded');}};
    const files=[createReadTool,createGrepTool,createFindTool,createLsTool].map(factory=>{
      const tool=factory(request.workspacePath);
      return {name:`kb_${tool.name}`,label:`KB ${tool.name}`,description:tool.description,parameters:tool.parameters,
        execute:async(id,args,signal,onUpdate)=>{budget();const p=await assertInside(request.workspacePath,args.path||'.');emit('status',{message:`检索知识库：${tool.name}`});return tool.execute(id,{...args,path:p},signal,onUpdate);}};
    });
    const remote=listed.tools.map(t=>({name:`mcp__business__${t.name}`,label:t.name,description:t.description||t.name,parameters:t.inputSchema,
      execute:async(_id,args,signal)=>{budget();emit('status',{message:`查询业务数据：${t.name}`});const r=await mcp.callTool({name:t.name,arguments:args},undefined,{signal,timeout:30000});return {content:r.content,details:{isError:Boolean(r.isError)}};}}));
    const modelRuntime=await ModelRuntime.create({authPath:path.join(home,'pi','auth.json'),modelsPath:null,refreshOnCreate:false,allowModelNetwork:false});
    modelRuntime.registerProvider('novex',{name:'Novex gateway',api:'openai-completions',baseUrl:config.baseUrl,apiKey:config.apiKey,models:[{id:config.model,name:config.model,reasoning:false,input:['text'],contextWindow:128000,maxTokens:config.maxTokens,cost:{input:0,output:0,cacheRead:0,cacheWrite:0},compat:{supportsDeveloperRole:false,supportsStore:false,maxTokensField:'max_tokens',thinkingFormat:'deepseek'}}]});
    const resourceLoader={getExtensions:()=>({extensions:[],errors:[],runtime:createExtensionRuntime()}),getSkills:()=>({skills:[],diagnostics:[]}),getPrompts:()=>({prompts:[],diagnostics:[]}),getThemes:()=>({themes:[],diagnostics:[]}),getAgentsFiles:()=>({agentsFiles:[]}),getSystemPrompt:()=>systemPrompt,getSystemPromptSource:()=>undefined,getAppendSystemPrompt:()=>[],getAppendSystemPromptSources:()=>[],extendResources:()=>{},reload:async()=>{}};
    ({session}=await createAgentSession({cwd:request.workspacePath,agentDir:path.join(home,'pi'),model:modelRuntime.getModel('novex',config.model),modelRuntime,thinkingLevel:'off',resourceLoader,tools:[...files,...remote].map(t=>t.name),customTools:[...files,...remote],sessionManager:SessionManager.inMemory(request.workspacePath),settingsManager:SettingsManager.inMemory({compaction:{enabled:false},retry:{enabled:false}})}));
    emit('status',{message:'Pi 已连接授权知识库与业务 MCP'});
    signal?.addEventListener('abort',()=>void session.abort(),{once:true});
    await session.prompt(input);
    const last=[...session.messages].reverse().find(m=>m.role==='assistant');
    if(!last||last.stopReason==='error'||last.stopReason==='aborted'||last.stopReason==='length')throw Object.assign(new Error(last?.errorMessage||`Pi did not complete: ${last?.stopReason||'no response'}`),{stopReason:last?.stopReason});
    const text=session.getLastAssistantText();
    if(!text?.trim())throw new Error('Pi returned no final answer');
    const usage={input:0,output:0,cacheRead:0,cacheWrite:0,reasoning:0,totalTokens:0};
    for(const message of session.messages)if(message.role==='assistant'&&message.usage)for(const key of Object.keys(usage))usage[key]+=message.usage[key]||0;
    return {text,usage:{...usage,toolCalls,runtime:'pi',version:'1.5.0'}};
  }catch(error){if(error&&typeof error==='object')error.toolCalls=toolCalls;throw error;
  }finally{session?.dispose();await mcp.close().catch(()=>{});}
}
