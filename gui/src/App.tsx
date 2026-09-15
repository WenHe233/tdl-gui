import { useEffect, useMemo, useRef, useState } from "react";
import { invoke, convertFileSrc } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { open } from "@tauri-apps/plugin-dialog";
import { openPath } from "@tauri-apps/plugin-opener";
import { Archive, Check, ChevronDown, CircleAlert, Download, File, FileAudio, FileImage, Film, FolderOpen, Image, LoaderCircle, LogIn, Menu, Pause, Play, Plus, RefreshCw, Search, Settings, SlidersHorizontal, X } from "lucide-react";
import { onWorkerEvent, rpc, startWorker } from "./rpc";
import type { Account, Bootstrap, Chat, DownloadPlan, Engine, Job, Media, Rule, WorkerEvent } from "./types";
import "./avatar.css";

const DEFAULT_TEMPLATE = '{{.AccountName}}/{{.ChatName}}/{{date .Date "2006-01-02"}}-{{.MessageID}}-{{.OriginalName}}';
const kinds = [["photo","图片"],["video","视频"],["audio","音频"],["voice","语音"],["document","文件"],["animation","动图"],["sticker","贴纸"]];
const kindIcon = (kind:string,size=22) => kind === "photo" ? <FileImage size={size}/> : kind === "video" || kind === "animation" ? <Film size={size}/> : kind === "audio" || kind === "voice" ? <FileAudio size={size}/> : <File size={size}/>;
const bytes = (n:number) => { if (!n) return "0 B"; const u=["B","KiB","MiB","GiB","TiB"]; const i=Math.min(Math.floor(Math.log(n)/Math.log(1024)),u.length-1); return `${(n/1024**i).toFixed(i?1:0)} ${u[i]}`; };
const dateInput = (v?:string) => {if(!v)return "";const d=new Date(v);if(Number.isNaN(d.getTime()))return "";return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,"0")}-${String(d.getDate()).padStart(2,"0")}`};
const dateBoundary = (v?:string,end=false) => {if(!v)return undefined;if(/^\d{4}-\d{2}-\d{2}$/.test(v)){const [y,m,d]=v.split("-").map(Number);return new Date(y,m-1,d,end?23:0,end?59:0,end?59:0,end?999:0)}const parsed=new Date(v);return Number.isNaN(parsed.getTime())?undefined:parsed};
const dateISO = (v?:string,end=false) => dateBoundary(v,end)?.toISOString();
const normalizedExt = (m:Media) => (m.extension||m.fileName.split(".").pop()||"").replace(/^\./,"").toLowerCase();
const hasExt = (items:string[]|undefined,ext:string) => !!items?.some(item=>item.trim().replace(/^\./,"").toLowerCase()===ext);
const makeID = (prefix:string) => `${prefix}_${crypto.randomUUID().replaceAll("-","").slice(0,16)}`;
const imageSource = (value:string) => value.startsWith("data:") ? value : convertFileSrc(value);
const shutdownApp = async()=>{try{await rpc("app.shutdown")}finally{try{await invoke("worker_stop")}finally{await invoke("quit_app")}}};

type LoginState = { open:boolean; accountId?:string; name:string; method:"qr"|"code"|"desktop"; phone:string; desktopPath:string; desktopPasscode:string; loginId?:string; qrCode?:string; prompt?:string; inputKind?:string; choices?:string[]; busy:boolean; error?:string };
const emptyLogin:LoginState = {open:false,name:"",method:"qr",phone:"",desktopPath:"",desktopPasscode:"",busy:false};

export default function App(){
  const [boot,setBoot] = useState<Bootstrap>();
  const [engine,setEngine] = useState<Engine>();
  const [accounts,setAccounts] = useState<Account[]>([]);
  const [active,setActive] = useState<Account>();
  const activeAccountId = useRef<string|undefined>(undefined);
  const [chats,setChats] = useState<Chat[]>([]);
  const [avatars,setAvatars] = useState<Record<string,string>>({});
  const requestedAvatars = useRef(new Set<string>());
  const [chat,setChat] = useState<Chat>();
  const [media,setMedia] = useState<Media[]>([]);
  const [mediaPreview,setMediaPreview] = useState<Media>();
  const [mediaOffset,setMediaOffset] = useState(0);
  const [hasMoreMedia,setHasMoreMedia] = useState(false);
  const [jobs,setJobs] = useState<Job[]>([]);
  const runningJobsRef = useRef<Job[]>([]);
  const [query,setQuery] = useState("");
  const [chatWindowStart,setChatWindowStart] = useState(0);
  const [loading,setLoading] = useState("正在启动…");
  const [error,setError] = useState<string>();
  const [login,setLogin] = useState<LoginState>(emptyLogin);
  const [plan,setPlan] = useState<DownloadPlan>();
  const [showPlan,setShowPlan] = useState(false);
  const [showJobs,setShowJobs] = useState(false);
  const [showSettings,setShowSettings] = useState(false);
  const [closeChoice,setCloseChoice] = useState(false);
  const [topicId,setTopicId] = useState("");
  const [rule,setRule] = useState<Partial<Rule>>({order:"oldest",kinds:[],template:DEFAULT_TEMPLATE,maxFiles:0,maxTotalSize:0,minFileSize:0,maxFileSize:0});

  const refreshBootstrap = async()=>{const b=await rpc<Bootstrap>("app.bootstrap");setBoot(b);setEngine(b.engine);setAccounts(b.accounts);setActive(b.activeAccount);setJobs(b.jobs);return b;};
  const loadChats = async(accountId?:string)=>{if(!accountId)return;const list=await rpc<Chat[]>("chats.list",{accountId,query:""});setChats(list)};
  const loadMedia = async(c:Chat,offset=0)=>{setChat(c);if(!offset)setTopicId("");setLoading("正在载入媒体索引…");try{const page=await rpc<{items:Media[];nextOffset:number;hasMore:boolean}>("media.list",{accountId:active?.id,chatId:c.id,offset,limit:100});setMedia(v=>offset?[...v,...page.items]:page.items);setMediaOffset(page.nextOffset);setHasMoreMedia(page.hasMore);const refs=page.items.filter(m=>["photo","video","animation","sticker"].includes(m.kind)&&!m.thumbPath).map(m=>({chatId:m.chatId,messageId:m.messageId}));if(refs.length&&active){rpc<Record<string,string>>("media.thumbnails",{accountId:active.id,items:refs}).then(paths=>setMedia(v=>v.map(m=>paths[m.messageId]?{...m,thumbPath:paths[m.messageId]}:m))).catch(()=>{})}}catch(e){setError(String(e))}finally{setLoading("")}};

  useEffect(()=>{let offEvent=()=>{};let offClose=()=>{};(async()=>{try{await startWorker();offEvent=onWorkerEvent(handleEvent);offClose=await listen("app-close-requested",()=>{if(runningJobsRef.current.length)setCloseChoice(true);else shutdownApp()});await refreshBootstrap();setLoading("")}catch(e){setError(String(e));setLoading("")}})();return()=>{offEvent();offClose()}},[]);
  useEffect(()=>{activeAccountId.current=active?.id;setAvatars({});requestedAvatars.current.clear();if(active)loadChats(active.id)},[active?.id]);
  useEffect(()=>{runningJobsRef.current=jobs.filter(j=>j.state==="running")},[jobs]);

  const handleEvent=(event:WorkerEvent)=>{
    if(event.type.startsWith("login.")){
      if(event.type==="login.completed"&&event.account){setLogin(emptyLogin);setActive(event.account);refreshBootstrap();return}
      if(event.type==="login.error"){setLogin(v=>({...v,busy:false,error:event.error,prompt:"登录失败"}));return}
      setLogin(v=>({...v,busy:false,loginId:event.loginId||v.loginId,qrCode:event.qrCode||v.qrCode,prompt:event.prompt,inputKind:event.type==="login.codeRequired"?"code":event.type==="login.passwordRequired"?"password":event.type==="login.desktopAccountRequired"?"desktopAccount":undefined,choices:event.choices}));
    }
    if(event.type==="job.updated"&&event.job)setJobs(v=>[event.job!,...v.filter(j=>j.id!==event.job!.id)]);
    if(event.type==="item.updated"&&event.item?.state==="done")setMedia(v=>v.map(m=>m.messageId===event.item!.messageId?{...m,downloaded:true,localPath:event.item!.targetPath}:m));
    if(event.type==="job.error")setError(event.message);
  };

  const filteredChats=useMemo(()=>{const q=query.toLowerCase();return chats.filter(c=>c.visibleName.trim()&&(!q||c.visibleName.toLowerCase().includes(q)||(c.username||"").toLowerCase().includes(q)))},[chats,query]);
  useEffect(()=>setChatWindowStart(0),[query]);
  useEffect(()=>{
    if(!active)return;
    const ids=filteredChats.slice(chatWindowStart,chatWindowStart+24).map(c=>c.id).filter(id=>!requestedAvatars.current.has(id));
    if(!ids.length)return;
    ids.forEach(id=>requestedAvatars.current.add(id));
    const accountId=active.id;
    rpc<Record<string,string>>("chats.avatars",{accountId,chatIds:ids}).then(found=>{
      if(activeAccountId.current===accountId)setAvatars(v=>({...v,...found}));
    }).catch(()=>ids.forEach(id=>requestedAvatars.current.delete(id)));
  },[active?.id,filteredChats,chatWindowStart]);
  const filteredMedia=useMemo(()=>{const matches=media.filter(m=>{
    if(rule.kinds?.length&&!rule.kinds.includes(m.kind))return false;
    if(rule.minFileSize&&m.size<rule.minFileSize)return false;if(rule.maxFileSize&&m.size>rule.maxFileSize)return false;
    const id=Number(m.messageId);if(rule.minMessageId&&id<rule.minMessageId)return false;if(rule.maxMessageId&&id>rule.maxMessageId)return false;
    const ext=normalizedExt(m);if(rule.includeExt?.length&&!hasExt(rule.includeExt,ext))return false;if(hasExt(rule.excludeExt,ext))return false;
    const hay=(m.fileName+"\n"+(m.caption||"")).toLowerCase();if(rule.includeKeyword&&!hay.includes(rule.includeKeyword.toLowerCase()))return false;if(rule.excludeKeyword&&hay.includes(rule.excludeKeyword.toLowerCase()))return false;
    const from=rule.recentDays?new Date(new Date().setDate(new Date().getDate()-rule.recentDays)):dateBoundary(rule.from);const to=dateBoundary(rule.to,true);if(from&&new Date(m.date)<from)return false;if(to&&new Date(m.date)>to)return false;return true;
  });if(rule.lastN&&matches.length>rule.lastN)return [...matches].sort((a,b)=>new Date(b.date).getTime()-new Date(a.date).getTime()).slice(0,rule.lastN);return matches},[media,rule]);
  const grouped=useMemo(()=>{const out:{key:string;items:Media[]}[]=[];for(const m of filteredMedia){const key=m.groupedId||m.messageId;const prev=out.at(-1);if(prev?.key===key)prev.items.push(m);else out.push({key,items:[m]})}return out},[filteredMedia]);

  const installEngine=async()=>{setLoading("正在从官方发布页安装 tdl…");setError(undefined);try{const v=await rpc<Engine>("engine.install",{version:"v0.20.4"});setEngine(v)}catch(e){setError(String(e))}finally{setLoading("")}};
  const chooseEngine=async()=>{const path=await open({multiple:false,filters:[{name:"tdl",extensions:["exe"]}]});if(typeof path==="string"){await rpc("engine.use",{version:path});await refreshBootstrap()}};
  const refreshChats=async()=>{if(!active)return;setLoading("正在从 Telegram 获取聊天列表…");try{setChats(await rpc("chats.refresh",{accountId:active.id}))}catch(e){setError(String(e))}finally{setLoading("")}};
  const scanMedia=async(rescan=false)=>{if(!active||!chat)return;setLoading(rescan?"正在重新扫描聊天历史…":"正在检查新增媒体…");try{await rpc("media.scan",{accountId:active.id,chatId:chat.id,topicId,from:dateISO(rule.from),to:dateISO(rule.to,true),lastN:rule.lastN||0,rescan});await loadMedia(chat)}catch(e){setError(String(e))}finally{setLoading("")}};
  const preview=async()=>{if(!active||!chat||!boot)return;const sameRule=rule.accountId===active.id&&rule.chatId===chat.id;const full:Rule={id:sameRule&&rule.id?rule.id:makeID("rule"),name:rule.name||`${chat.visibleName} 下载`,accountId:active.id,chatId:chat.id,topicId,order:rule.order||"oldest",timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,rootDir:rule.rootDir||boot.settings.downloadRoot,template:rule.template||DEFAULT_TEMPLATE,from:dateISO(rule.from),to:dateISO(rule.to,true),recentDays:rule.recentDays||0,lastN:rule.lastN||0,minMessageId:rule.minMessageId||0,maxMessageId:rule.maxMessageId||0,kinds:rule.kinds||[],includeExt:rule.includeExt||[],excludeExt:rule.excludeExt||[],includeKeyword:rule.includeKeyword||"",excludeKeyword:rule.excludeKeyword||"",minFileSize:rule.minFileSize||0,maxFileSize:rule.maxFileSize||0,maxFiles:rule.maxFiles||0,maxTotalSize:rule.maxTotalSize||0};setLoading("正在生成固定下载清单…");try{await rpc("rules.save",full);setRule(full);setBoot(v=>v?{...v,rules:[full,...v.rules.filter(saved=>saved.id!==full.id)]}:v);const p=await rpc<DownloadPlan>("media.preview",{ruleId:full.id});setPlan(p);setShowPlan(true)}catch(e){setError(String(e))}finally{setLoading("")}};
  const togglePlan=(messageId:string)=>setPlan(p=>{if(!p)return p;const items=p.items.map(x=>x.media.messageId===messageId&&["selected","excluded"].includes(x.status)?{...x,selected:!x.selected,status:x.selected?"excluded":"selected",reason:x.selected?"手动排除":undefined}:x);return{...p,items,selectedFiles:items.filter(x=>x.selected).length,selectedBytes:items.filter(x=>x.selected).reduce((n,x)=>n+x.media.size,0)}});
  const startDownload=async()=>{if(!plan)return;setLoading("正在创建下载任务…");try{await rpc("plans.save",plan);const job=await rpc<Job>("jobs.create",{planId:plan.id});setJobs(v=>[job,...v]);await rpc("jobs.start",{id:job.id});setShowPlan(false);setShowJobs(true)}catch(e){setError(String(e))}finally{setLoading("")}};
  const switchAccount=async(id:string)=>{await rpc("accounts.use",{id});const a=accounts.find(x=>x.id===id);setActive(a);setChat(undefined);setMedia([])};
  const beginLogin=async()=>{setLogin(v=>({...v,busy:true,error:undefined}));try{let accountId=login.accountId;if(!accountId){const a=await rpc<Account>("accounts.add",{name:login.name||"Telegram 账户",namespace:""});accountId=a.id;setAccounts(v=>[...v,a])}const result=await rpc<{loginId:string}>("accounts.login.start",{accountId,method:login.method,phone:login.phone,desktopPath:login.desktopPath,desktopPasscode:login.desktopPasscode});setLogin(v=>({...v,accountId,loginId:result.loginId,prompt:login.method==="qr"?"正在生成二维码…":"正在连接 Telegram…"}))}catch(e){setLogin(v=>({...v,busy:false,error:String(e)}))}};
  const submitLogin=async(value:string,kind=login.inputKind)=>{if(!login.loginId||!kind)return;setLogin(v=>({...v,busy:true}));try{await rpc("accounts.login.submit",{loginId:login.loginId,kind,value})}catch(e){setLogin(v=>({...v,busy:false,error:String(e)}))}};
  const runningJobs=jobs.filter(j=>j.state==="running");
  const exitSaving=shutdownApp;

  return <div className="app-shell">
    <aside className="sidebar">
      <div className="account-bar">
        <button className="icon-button"><Menu size={22}/></button>
        <select value={active?.id||""} onChange={e=>switchAccount(e.target.value)} aria-label="当前账户"><option value="">选择账户</option>{accounts.map(a=><option key={a.id} value={a.id}>{a.displayName}</option>)}</select>
        <button className="icon-button" title="添加账户" onClick={()=>setLogin({...emptyLogin,open:true})}><Plus size={20}/></button>
      </div>
      <div className="search"><Search size={17}/><input value={query} onChange={e=>setQuery(e.target.value)} placeholder="搜索聊天"/><button onClick={refreshChats} title="刷新聊天"><RefreshCw size={16}/></button></div>
      <div className="chat-list" onScroll={e=>setChatWindowStart(Math.max(0,Math.floor(e.currentTarget.scrollTop/66)-3))}>{filteredChats.map(c=><button key={c.id} className={`chat-row ${chat?.id===c.id?"active":""}`} onClick={()=>loadMedia(c)}><span className={`avatar ${c.type}`}>{avatars[c.id]?<img src={imageSource(avatars[c.id])} alt=""/>:c.visibleName.slice(0,1).toUpperCase()}</span><span><strong>{c.visibleName}</strong><small>{c.username?`@${c.username}`:c.type}</small></span></button>)}{active&&!chats.length&&<div className="empty-small">刷新以载入聊天列表</div>}</div>
      <div className="sidebar-footer"><button onClick={()=>setShowJobs(true)}><Download size={18}/>下载任务{runningJobs.length>0&&<b>{runningJobs.length}</b>}</button><button onClick={()=>setShowSettings(true)}><Settings size={18}/>设置</button></div>
    </aside>

    <main className="conversation">
      <header className="conversation-header">{chat?<><div><h2>{chat.visibleName}</h2><span>当前已加载 {filteredMedia.length} 条媒体消息</span></div><div className="header-actions">{chat.topics&&chat.topics.length>0&&<select value={topicId} onChange={e=>setTopicId(e.target.value)}><option value="">全部话题</option>{chat.topics.map(t=><option key={t.id} value={t.id}>{t.title}</option>)}</select>}<button onClick={()=>scanMedia(false)}><RefreshCw size={17}/>扫描新增</button><button onClick={()=>scanMedia(true)}><Archive size={17}/>重扫历史</button><button className="primary" onClick={preview}><Download size={17}/>预览全部筛选结果</button></div></>:<div><h2>TDL Media</h2><span>选择一个聊天开始浏览</span></div>}</header>
      <section className="message-stream">{!chat?<Welcome engine={engine} onInstall={installEngine} onChoose={chooseEngine} onLogin={()=>setLogin({...emptyLogin,open:true})} hasAccount={!!active}/>:grouped.length?<>{grouped.map(group=><MediaGroup key={group.key} items={group.items} onOpen={m=>m.localPath?openPath(m.localPath):m.thumbPath&&setMediaPreview(m)}/>)}{hasMoreMedia&&<button className="load-more" onClick={()=>loadMedia(chat,mediaOffset)}><ChevronDown/>加载更早的媒体</button>}</>:<div className="empty-state"><Archive size={54}/><h3>尚未索引媒体</h3><p>点击“扫描媒体”从这个聊天读取媒体消息。</p></div>}</section>
    </main>

    <aside className="filters-panel">
      <div className="panel-title"><SlidersHorizontal size={19}/><strong>筛选与保存</strong></div>
      <label>保存的规则<select value={rule.id||""} onChange={e=>{const saved=boot?.rules.find(r=>r.id===e.target.value);if(saved){setRule(saved);const target=chats.find(c=>c.id===saved.chatId);if(target)loadMedia(target)}else setRule(v=>({...v,id:undefined}))}}><option value="">选择已有规则…</option>{boot?.rules.filter(r=>!active||r.accountId===active.id).map(r=><option key={r.id} value={r.id}>{r.name}</option>)}</select></label>
      <label>规则名称<input value={rule.name||""} onChange={e=>setRule(v=>({...v,name:e.target.value}))} placeholder={chat?`${chat.visibleName} 下载`:"下载规则"}/></label>
      <label>时间范围<span><input type="date" value={dateInput(rule.from)} onChange={e=>setRule(v=>({...v,from:e.target.value}))}/><input type="date" value={dateInput(rule.to)} onChange={e=>setRule(v=>({...v,to:e.target.value}))}/></span></label>
      <label>最近天数<input type="number" min="0" value={rule.recentDays||""} onChange={e=>setRule(v=>({...v,recentDays:Number(e.target.value)}))} placeholder="不限制"/></label>
      <label>最近媒体数<input type="number" min="0" value={rule.lastN||""} onChange={e=>setRule(v=>({...v,lastN:Number(e.target.value)}))} placeholder="不限制"/></label>
      <div className="two-cols"><label>消息 ID 从<input type="number" min="0" value={rule.minMessageId||""} onChange={e=>setRule(v=>({...v,minMessageId:Number(e.target.value)}))}/></label><label>消息 ID 到<input type="number" min="0" value={rule.maxMessageId||""} onChange={e=>setRule(v=>({...v,maxMessageId:Number(e.target.value)}))}/></label></div>
      <fieldset><legend>媒体类型</legend><div className="kind-grid">{kinds.map(([id,name])=><label key={id} className={rule.kinds?.includes(id)?"selected":""}><input type="checkbox" checked={rule.kinds?.includes(id)||false} onChange={()=>setRule(v=>({...v,kinds:v.kinds?.includes(id)?v.kinds.filter(x=>x!==id):[...(v.kinds||[]),id]}))}/>{kindIcon(id,16)}{name}</label>)}</div></fieldset>
      <label>包含关键词<input value={rule.includeKeyword||""} onChange={e=>setRule(v=>({...v,includeKeyword:e.target.value}))} placeholder="文件名或消息文字"/></label>
      <label>排除关键词<input value={rule.excludeKeyword||""} onChange={e=>setRule(v=>({...v,excludeKeyword:e.target.value}))}/></label>
      <label>只包含扩展名<input value={(rule.includeExt||[]).join(",")} onChange={e=>setRule(v=>({...v,includeExt:e.target.value.split(",").map(x=>x.trim()).filter(Boolean)}))} placeholder="jpg,png,mp4"/></label>
      <label>排除扩展名<input value={(rule.excludeExt||[]).join(",")} onChange={e=>setRule(v=>({...v,excludeExt:e.target.value.split(",").map(x=>x.trim()).filter(Boolean)}))} placeholder="zip,exe"/></label>
      <div className="two-cols"><label>最小 MiB<input type="number" min="0" value={rule.minFileSize?rule.minFileSize/1048576:""} onChange={e=>setRule(v=>({...v,minFileSize:Number(e.target.value)*1048576}))}/></label><label>最大 MiB<input type="number" min="0" value={rule.maxFileSize?rule.maxFileSize/1048576:""} onChange={e=>setRule(v=>({...v,maxFileSize:Number(e.target.value)*1048576}))}/></label></div>
      <div className="two-cols"><label>最多文件<input type="number" min="0" value={rule.maxFiles||""} onChange={e=>setRule(v=>({...v,maxFiles:Number(e.target.value)}))}/></label><label>总量 GiB<input type="number" min="0" value={rule.maxTotalSize?rule.maxTotalSize/1073741824:""} onChange={e=>setRule(v=>({...v,maxTotalSize:Number(e.target.value)*1073741824}))}/></label></div>
      <label>下载顺序<select value={rule.order} onChange={e=>setRule(v=>({...v,order:e.target.value}))}><option value="oldest">从旧到新</option><option value="newest">从新到旧</option></select></label>
      <label>下载根目录<span className="path-input"><input value={rule.rootDir||boot?.settings.downloadRoot||""} onChange={e=>setRule(v=>({...v,rootDir:e.target.value}))}/><button onClick={async()=>{const p=await open({directory:true});if(typeof p==="string")setRule(v=>({...v,rootDir:p}))}}><FolderOpen size={17}/></button></span></label>
      <label>命名模板<textarea rows={3} value={rule.template} onChange={e=>setRule(v=>({...v,template:e.target.value}))}/></label>
      <button className="primary full" disabled={!chat} onClick={preview}><Download size={18}/>生成下载清单</button>
    </aside>

    {loading&&<div className="loading-toast"><LoaderCircle className="spin" size={18}/>{loading}</div>}
    {error&&<div className="error-toast"><CircleAlert size={18}/><span>{error}</span><button onClick={()=>setError(undefined)}><X size={17}/></button></div>}
    {login.open&&<LoginModal state={login} accounts={accounts} setState={setLogin} onStart={beginLogin} onSubmit={submitLogin} onClose={()=>{if(login.loginId)rpc("accounts.login.cancel",{loginId:login.loginId});setLogin(emptyLogin)}}/>}
    {mediaPreview?.thumbPath&&<div className="modal-backdrop image-preview" onClick={()=>setMediaPreview(undefined)}><button className="modal-close"><X/></button><img src={imageSource(mediaPreview.thumbPath)} alt={mediaPreview.fileName}/><p>{mediaPreview.fileName}</p></div>}
    {showPlan&&plan&&<PlanDrawer plan={plan} onToggle={togglePlan} onClose={()=>setShowPlan(false)} onStart={startDownload}/>}
    {showJobs&&<JobsDrawer jobs={jobs} onClose={()=>setShowJobs(false)} onPause={id=>rpc("jobs.pause",{id})} onResume={id=>rpc("jobs.start",{id})}/>}
    {showSettings&&boot&&<SettingsModal settings={boot.settings} engine={engine} onClose={()=>setShowSettings(false)} onInstall={installEngine} onChoose={chooseEngine} onClear={async()=>{await rpc("cache.clear");setMedia(v=>v.map(m=>({...m,thumbPath:""})))}}/>}
    {closeChoice&&<div className="modal-backdrop"><div className="confirm-card"><h3>下载仍在进行</h3><p>你可以让任务留在托盘继续运行，或者保存当前进度后退出。</p><div><button onClick={exitSaving}>保存进度并退出</button><button className="primary" onClick={async()=>{setCloseChoice(false);await invoke("hide_main")}}>后台继续下载</button></div></div></div>}
  </div>
}

function Welcome({engine,onInstall,onChoose,onLogin,hasAccount}:{engine?:Engine;onInstall:()=>void;onChoose:()=>void;onLogin:()=>void;hasAccount:boolean}){return <div className="welcome"><div className="paper-plane">➤</div><h1>Telegram 媒体，清楚地下载</h1><p>浏览聊天中的媒体，按时间、类型和大小筛选，再用官方 tdl 引擎可靠下载。</p>{!engine?<div className="welcome-actions"><button className="primary" onClick={onInstall}><Download size={18}/>安装 tdl v0.20.4</button><button onClick={onChoose}><FolderOpen size={18}/>使用已有 tdl.exe</button></div>:!hasAccount?<button className="primary" onClick={onLogin}><LogIn size={18}/>登录 Telegram</button>:<p className="ready"><Check size={18}/>已就绪，请在左侧刷新并选择聊天</p>}</div>}

function MediaGroup({items,onOpen}:{items:Media[];onOpen:(m:Media)=>void}){const first=items[0];return <article className="message-card"><div className={`media-grid count-${Math.min(items.length,4)}`}>{items.map(m=><button key={m.messageId} className="media-tile" onClick={()=>onOpen(m)}>{m.thumbPath?<img src={imageSource(m.thumbPath)} alt={m.fileName}/>:<span>{kindIcon(m.kind,38)}</span>}<i>{bytes(m.size)}</i>{m.downloaded&&<em><Check size={13}/></em>}</button>)}</div><div className="message-info"><strong>{items.length>1?`${items.length} 个媒体`:first.fileName}</strong>{first.caption&&<p>{first.caption}</p>}<small>{new Date(first.date).toLocaleString()} · {items.map(x=>bytes(x.size)).join(" + ")}</small></div></article>}

function LoginModal({state,accounts,setState,onStart,onSubmit,onClose}:{state:LoginState;accounts:Account[];setState:React.Dispatch<React.SetStateAction<LoginState>>;onStart:()=>void;onSubmit:(v:string,k?:string)=>void;onClose:()=>void}){const [value,setValue]=useState("");return <div className="modal-backdrop"><div className="modal login-modal"><button className="modal-close" onClick={onClose}><X/></button><h2>登录 Telegram</h2><p>凭据只保存在本机，不会进入日志和下载报告。</p>{!state.loginId?<><label>账户配置<select value={state.accountId||"new"} onChange={e=>setState(v=>({...v,accountId:e.target.value==="new"?undefined:e.target.value}))}><option value="new">添加新账户</option>{accounts.map(a=><option key={a.id} value={a.id}>{a.displayName}</option>)}</select></label>{!state.accountId&&<label>显示名称<input value={state.name} onChange={e=>setState(v=>({...v,name:e.target.value}))} placeholder="我的 Telegram"/></label>}<div className="login-tabs">{[["qr","扫码"],["code","验证码"],["desktop","桌面导入"]].map(([id,name])=><button key={id} className={state.method===id?"active":""} onClick={()=>setState(v=>({...v,method:id as LoginState["method"]}))}>{name}</button>)}</div>{state.method==="code"&&<label>手机号<input value={state.phone} onChange={e=>setState(v=>({...v,phone:e.target.value}))} placeholder="+86 138…"/></label>}{state.method==="desktop"&&<><label>Telegram Desktop 目录<input value={state.desktopPath} onChange={e=>setState(v=>({...v,desktopPath:e.target.value}))} placeholder="留空自动查找"/></label><label>Desktop 本地密码<input type="password" value={state.desktopPasscode} onChange={e=>setState(v=>({...v,desktopPasscode:e.target.value}))} placeholder="未设置则留空"/></label></>}<button className="primary full" disabled={state.busy||(!state.accountId&&!state.name)||(state.method==="code"&&!state.phone)} onClick={onStart}>{state.busy?<LoaderCircle className="spin"/>:<LogIn/>}开始登录</button></>:<div className="login-step">{state.qrCode&&<img className="qr" src={state.qrCode}/>}<h3>{state.prompt}</h3>{state.choices?.map(c=><button className="choice" key={c} onClick={()=>onSubmit(c,"desktopAccount")}>{c}</button>)}{state.inputKind&&state.inputKind!=="desktopAccount"&&<form onSubmit={e=>{e.preventDefault();onSubmit(value);setValue("")}}><input autoFocus type={state.inputKind==="password"?"password":"text"} value={value} onChange={e=>setValue(e.target.value)} placeholder={state.inputKind==="code"?"验证码":"两步验证密码"}/><button className="primary" disabled={!value||state.busy}>提交</button></form>}</div>}{state.error&&<p className="inline-error">{state.error}</p>}</div></div>}

function PlanDrawer({plan,onToggle,onClose,onStart}:{plan:DownloadPlan;onToggle:(id:string)=>void;onClose:()=>void;onStart:()=>void}){return <div className="drawer-backdrop"><aside className="drawer plan-drawer"><header><div><h2>下载清单</h2><p>{plan.selectedFiles} 个文件 · {bytes(plan.selectedBytes)}，{plan.existingFiles} 个已存在</p></div><button onClick={onClose}><X/></button></header><div className="plan-list">{plan.items.map(x=><label key={x.media.messageId} className={`plan-row ${x.selected?"selected":""}`}><input type="checkbox" disabled={!['selected','excluded'].includes(x.status)} checked={x.selected} onChange={()=>onToggle(x.media.messageId)}/><span className="plan-kind">{kindIcon(x.media.kind)}</span><span><strong>{x.media.fileName}</strong><small>{new Date(x.media.date).toLocaleString()} · {bytes(x.media.size)}</small><small className="target">{x.targetPath||x.reason}</small></span><em>{x.selected?"下载":x.reason||x.status}</em></label>)}</div><footer><span>固定清单创建后，恢复任务仍使用这份清单。</span><button className="primary" disabled={!plan.selectedFiles} onClick={onStart}><Download/>开始下载</button></footer></aside></div>}

function JobsDrawer({jobs,onClose,onPause,onResume}:{jobs:Job[];onClose:()=>void;onPause:(id:string)=>void;onResume:(id:string)=>void}){return <div className="drawer-backdrop"><aside className="drawer jobs-drawer"><header><div><h2>下载任务</h2><p>进行中与历史任务</p></div><button onClick={onClose}><X/></button></header><div className="job-list">{jobs.map(j=>{const pct=j.totalBytes?Math.round(j.doneBytes/j.totalBytes*100):0;return <article key={j.id}><div className="job-top"><span className={`state ${j.state}`}>{j.state}</span><strong>{j.doneFiles}/{j.totalFiles} 个文件</strong><small>{bytes(j.doneBytes)} / {bytes(j.totalBytes)}</small></div><div className="progress"><i style={{width:`${pct}%`}}/></div>{j.error&&<p>{j.error}</p>}<div className="job-actions">{j.state==="running"?<button onClick={()=>onPause(j.id)}><Pause/>暂停</button>:!["completed","cancelled"].includes(j.state)&&<button onClick={()=>onResume(j.id)}><Play/>恢复/重试</button>}<span>{new Date(j.createdAt).toLocaleString()}</span></div></article>})}{!jobs.length&&<div className="empty-state"><Download/><p>还没有下载任务</p></div>}</div></aside></div>}

function SettingsModal({settings,engine,onClose,onInstall,onChoose,onClear}:{settings:Bootstrap["settings"];engine?:Engine;onClose:()=>void;onInstall:()=>void;onChoose:()=>void;onClear:()=>void}){return <div className="modal-backdrop"><div className="modal settings-modal"><button className="modal-close" onClick={onClose}><X/></button><h2>设置与引擎</h2><dl><dt>数据目录</dt><dd>{settings.dataDir}</dd><dt>默认下载目录</dt><dd>{settings.downloadRoot}</dd><dt>缓存上限</dt><dd>{bytes(settings.cacheMaxBytes)}</dd><dt>重试次数</dt><dd>{settings.retries}</dd><dt>tdl 引擎</dt><dd>{engine?`${engine.version} · ${engine.path}`:"尚未安装"}</dd></dl><div className="modal-actions"><button onClick={onClear}><X/>清理预览缓存</button><button onClick={onChoose}><FolderOpen/>指定 tdl.exe</button><button className="primary" onClick={onInstall}><RefreshCw/>安装/更新 v0.20.4</button></div></div></div>}
