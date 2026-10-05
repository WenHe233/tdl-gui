import { beforeEach, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import App from "../src/App";
import { rpc } from "../src/rpc";
import type { Bootstrap, Chat, Media, WorkerEvent } from "../src/types";

const mocks = vi.hoisted(() => ({
  listeners: new Set<(e: WorkerEvent) => void>(),
}));
vi.mock("../src/rpc", () => ({
  rpc: vi.fn(),
  startWorker: vi.fn(async () => {}),
  onWorkerEvent: (fn: (e: WorkerEvent) => void) => {
    mocks.listeners.add(fn);
    return () => mocks.listeners.delete(fn);
  },
}));
vi.mock("@tauri-apps/api/core", () => ({
  invoke: vi.fn(),
  convertFileSrc: (s: string) => s,
}));
vi.mock("@tauri-apps/api/event", () => ({
  listen: vi.fn(async () => () => {}),
}));
vi.mock("@tauri-apps/plugin-dialog", () => ({ open: vi.fn() }));
vi.mock("@tauri-apps/plugin-opener", () => ({
  openPath: vi.fn(),
  revealItemInDir: vi.fn(),
}));

const chats: Chat[] = [
  { id: "A", accountId: "a", visibleName: "测试聊天A", type: "channel" },
  { id: "B", accountId: "a", visibleName: "测试聊天B", type: "channel" },
];
const boot: Bootstrap = {
  version: "0.2.0",
  protocolVersion: "1.0",
  settings: {
    dataDir: "data",
    downloadRoot: "downloads",
    fileConcurrency: 1,
    retries: 1,
    cacheMaxBytes: 1,
  },
  accounts: [
    { id: "a", namespace: "a", displayName: "测试账户", active: true },
  ],
  activeAccount: {
    id: "a",
    namespace: "a",
    displayName: "测试账户",
    active: true,
  },
  engine: { path: "tdl.exe", version: "v0.20.4", active: true },
  rules: [],
  jobs: [],
};
const items = (chatId: string) =>
  Array.from({ length: 125 }, (_, i): Media => ({
    accountId: "a",
    chatId,
    messageId: String(i + 1),
    mediaId: String(i + 1),
    kind: "document",
    fileName: `${chatId}-${i + 1}.bin`,
    size: 3,
    date: "2026-01-01T00:00:00Z",
    downloaded: false,
  }));
const base = async (method: string, p: any): Promise<any> => {
  if (method === "app.bootstrap") return boot;
  if (method === "chats.list") return chats;
  if (method === "chats.avatars" || method === "media.thumbnails") return {};
  if (method === "media.list")
    return {
      items: items(p.chatId).slice(p.offset, p.offset + 100),
      nextOffset: Math.min(p.offset + 100, 125),
      hasMore: p.offset === 0,
    };
  if (method === "media.previewSelection")
    return {
      id: "p",
      items: p.messageIds.map((id: string) => ({
        media: items(p.rule.chatId)[Number(id) - 1],
        selected: true,
        status: "selected",
        targetPath: "downloads/file",
      })),
      selectedFiles: p.messageIds.length,
      selectedBytes: p.messageIds.length * 3,
      existingFiles: 0,
    };
  return true;
};
beforeEach(() => {
  vi.clearAllMocks();
  mocks.listeners.clear();
  vi.mocked(rpc).mockImplementation(base);
});

it("treats zero timestamps from saved Go rules as unbounded",async()=>{
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>method==="app.bootstrap"?{...boot,rules:[{id:"saved",name:"无日期限制",accountId:"a",chatId:"A",order:"oldest",rootDir:"downloads",template:"",timezone:"UTC",from:"0001-01-01T00:00:00Z",to:"0001-01-01T00:00:00Z"}]}:base(method,p));
  render(<App/>);await screen.findByText("测试聊天A");
  fireEvent.change(screen.getByLabelText("保存的规则"),{target:{value:"saved"}});
  await screen.findByRole("button",{name:"预览 A-1.bin"});
  expect((screen.getByLabelText("开始时间") as HTMLInputElement).value).toBe("");
  expect((screen.getByLabelText("结束时间") as HTMLInputElement).value).toBe("");
});

it("selects across 125 records and submits hidden selections", async () => {
  render(<App />);
  fireEvent.click(await screen.findByText("测试聊天A"));
  fireEvent.contextMenu(
    await screen.findByRole("button", { name: "预览 A-1.bin" }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "选择消息" }));
  fireEvent.click(screen.getByText("加载更早的媒体"));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "选择 A-125.bin" }),
  );
  fireEvent.change(screen.getByLabelText("包含关键词"), {
    target: { value: "A-125" },
  });
  expect(screen.getByText("含隐藏 1 项")).toBeTruthy();
  fireEvent.click(screen.getByText("下载所选"));
  await screen.findByText("下载清单");
  expect(
    vi
      .mocked(rpc)
      .mock.calls.find(([method]) => method === "media.previewSelection")?.[1],
  ).toMatchObject({ messageIds: ["1", "125"] });
});

it("keeps load-more available when all loaded records are filtered out", async () => {
  render(<App />);
  fireEvent.click(await screen.findByText("测试聊天A"));
  await screen.findByRole("button", { name: "预览 A-1.bin" });
  fireEvent.change(screen.getByLabelText("包含关键词"), {
    target: { value: "not-present" },
  });
  expect(screen.getByText("没有符合条件的媒体")).toBeTruthy();
  expect(screen.getByText("加载更早的媒体")).toBeTruthy();
});

it("discards old chat responses and isolates completion events by account and chat", async () => {
  let resolveA!: (v: any) => void;
  vi.mocked(rpc).mockImplementation(async (method, p: any) =>
    method === "media.list" && p.chatId === "A"
      ? await new Promise((resolve) => {
          resolveA = resolve;
        })
      : base(method, p),
  );
  render(<App />);
  fireEvent.click(await screen.findByText("测试聊天A"));
  await waitFor(() => expect(resolveA).toBeTypeOf("function"));
  fireEvent.click(screen.getByText("测试聊天B"));
  await screen.findByRole("button", { name: "预览 B-1.bin" });
  await act(async () =>
    resolveA({ items: items("A"), nextOffset: 125, hasMore: false }),
  );
  expect(screen.queryByRole("button", { name: "预览 A-1.bin" })).toBeNull();
  act(() =>
    mocks.listeners.forEach((fn) =>
      fn({
        type: "item.updated",
        accountId: "a",
        item: {
          jobId: "j",
          chatId: "A",
          messageId: "1",
          mediaId: "1",
          state: "done",
          targetPath: "file",
          size: 3,
          attempts: 1,
        },
      }),
    ),
  );
  fireEvent.change(screen.getByLabelText("下载状态"), {
    target: { value: "done" },
  });
  expect(screen.queryByRole("button", { name: "预览 B-1.bin" })).toBeNull();
});

it("opens context actions only on right click and fills exact time before confirmation", async () => {
  render(<App />);
  fireEvent.click(await screen.findByText("测试聊天A"));
  const card = await screen.findByRole("button", { name: "预览 A-1.bin" });
  expect(screen.queryByText("下载所选")).toBeNull();
  expect(screen.queryByText("从此条开始下载")).toBeNull();
  expect(screen.queryByRole("checkbox", { name: "选择 A-1.bin" })).toBeNull();
  fireEvent.contextMenu(card, { clientX: 200, clientY: 200 });
  fireEvent.click(screen.getByRole("menuitem", { name: "从此条开始下载" }));
  expect(screen.getByRole("dialog")).toBeTruthy();
  const start = (screen.getByLabelText("开始时间") as HTMLInputElement).value;
  expect(new Date(start).toISOString()).toBe("2026-01-01T00:00:00.000Z");
  expect(
    (screen.getByLabelText("结束时间") as HTMLInputElement).value,
  ).not.toBe("");
  expect(
    vi.mocked(rpc).mock.calls.some(([m]) => m === "media.previewAfter"),
  ).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect((screen.getByLabelText("开始时间") as HTMLInputElement).value).toBe(
    start,
  );
});

it("starts one download only after confirmation and a successful scan",async()=>{
  const plan={id:"after",accountId:"a",chatId:"A",selectedFiles:1,selectedBytes:3,existingFiles:0,items:[{media:items("A")[0],selected:true,status:"selected",targetPath:"downloads/a"}]};
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="media.previewAfter")return {operationId:"op"};
    if(method==="media.operation.get")return {operationId:"op",state:"completed",plan};
    if(method==="jobs.create")return {id:"j",state:"queued",totalFiles:1,doneFiles:0,totalBytes:3,doneBytes:0,failedFiles:0};
    return base(method,p);
  });
  render(<App/>);fireEvent.click(await screen.findByText("测试聊天A"));
  fireEvent.contextMenu(await screen.findByRole("button",{name:"预览 A-1.bin"}));fireEvent.click(screen.getByRole("menuitem",{name:"从此条开始下载"}));
  expect(vi.mocked(rpc).mock.calls.some(([m])=>m==="jobs.create")).toBe(false);
  fireEvent.click(screen.getByRole("button",{name:"开始下载",exact:true}));
  await waitFor(()=>expect(vi.mocked(rpc).mock.calls.filter(([m])=>m==="jobs.start")).toHaveLength(1));
  expect(vi.mocked(rpc).mock.calls.find(([m])=>m==="media.previewAfter")?.[1]).toMatchObject({anchorMessageId:"1",rule:{recentDays:0,lastN:0,minMessageId:0,maxMessageId:0}});
  expect(screen.queryByRole("dialog")).toBeNull();
});


it("reloads media in the chosen order and retains selected messages", async () => {
  render(<App />); fireEvent.click(await screen.findByText("测试聊天A"));
  fireEvent.contextMenu(await screen.findByRole("button", {name:"预览 A-1.bin"}));
  fireEvent.click(screen.getByRole("menuitem", {name:"选择消息"}));
  fireEvent.change(screen.getByLabelText("消息顺序"), {target:{value:"oldest"}});
  await waitFor(() => expect(vi.mocked(rpc).mock.calls.some(([m,p]: any) => m === "media.list" && p.order === "oldest" && p.offset === 0)).toBe(true));
  expect(await screen.findByText("加载更新的媒体")).toBeTruthy();
  expect(screen.getByText("已选 1 项")).toBeTruthy();
  expect((screen.getByLabelText("下载顺序") as HTMLSelectElement).value).toBe("oldest");
});

it("keeps account history removal behind an explicit confirmation", async () => {
  render(<App />); await screen.findByText("测试聊天A");
  fireEvent.click(screen.getByTitle("添加账户"));
  fireEvent.change(screen.getByLabelText("账户配置"), {target:{value:"a"}});
  fireEvent.click(screen.getByRole("button", {name:"删除登录信息"}));
  expect(screen.getByRole("dialog").textContent).toContain("索引、规则、任务和下载文件保留");
  fireEvent.click(screen.getByRole("button", {name:"取消",exact:true}));
  expect(vi.mocked(rpc).mock.calls.some(([m]) => m === "accounts.remove")).toBe(false);
  fireEvent.click(screen.getByRole("button", {name:"删除登录信息"}));
  const dialog=screen.getByRole("dialog");
  fireEvent.click(dialog.querySelector(".danger")!);
  await waitFor(() => expect(vi.mocked(rpc).mock.calls.filter(([m]) => m === "accounts.remove")).toHaveLength(1));
});

const archivedChats: Chat[] = [{...chats[0],archived:true,topics:[{id:"5",title:"归档话题"}]},chats[1]];
const archiveFolders = [{id:"all",title:"全部聊天",chatIds:["B"],pinnedIds:[]},
  {id:"archived",title:"已归档",chatIds:["A"],pinnedIds:[]},
  {id:"work",title:"工作",chatIds:["A","B"],pinnedIds:[]}];

it("restores the cached archive before refresh and keeps it usable when offline", async () => {
  let failRefresh!: (reason: Error) => void;
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="chats.list")return archivedChats;
    if(method==="chats.folders")return {folders:archiveFolders,selectedFolderId:"archived"};
    if(method==="chats.refresh")return new Promise((_,reject)=>{failRefresh=reject;});
    return base(method,p);
  });
  render(<App/>);
  await screen.findByText("测试聊天A");
  expect(screen.queryByText("测试聊天B")).toBeNull();
  expect(screen.getByRole("button",{name:"已归档"}).getAttribute("aria-pressed")).toBe("true");
  await waitFor(()=>expect(failRefresh).toBeTypeOf("function"));
  await act(async()=>failRefresh(new Error("offline")));
  await screen.findByText(/聊天列表更新失败/);
  expect(screen.getByText("测试聊天A")).toBeTruthy();
  fireEvent.click(screen.getByRole("button",{name:"全部聊天"}));
  expect(screen.getByText("测试聊天B")).toBeTruthy();
  expect(screen.queryByText("测试聊天A")).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"工作"}));
  expect(screen.getByText("测试聊天A")).toBeTruthy();
  expect(screen.getByText("测试聊天B")).toBeTruthy();
});

it("keeps a user folder choice made while saved preferences are still loading",async()=>{
  let resolveFolders!: (value: unknown)=>void;
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="chats.list"||method==="chats.refresh")return archivedChats;
    if(method==="chats.folders")return new Promise(resolve=>{resolveFolders=resolve;});
    return base(method,p);
  });
  render(<App/>);
  await waitFor(()=>expect(resolveFolders).toBeTypeOf("function"));
  fireEvent.click(screen.getByRole("button",{name:"已归档"}));
  await act(async()=>resolveFolders({folders:archiveFolders,selectedFolderId:"all"}));
  await screen.findByText("测试聊天A");
  expect(screen.getByRole("button",{name:"已归档"}).getAttribute("aria-pressed")).toBe("true");
});

it("keeps archive browsing, topics and download selection working across folder changes",async()=>{
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="chats.list"||method==="chats.refresh")return archivedChats;
    if(method==="chats.folders")return {folders:archiveFolders,selectedFolderId:"archived"};
    return base(method,p);
  });
  render(<App/>);fireEvent.click(await screen.findByText("测试聊天A"));
  await screen.findByRole("button",{name:"预览 A-1.bin"});
  fireEvent.change(screen.getByRole("option",{name:"归档话题"}).parentElement!,{target:{value:"5"}});
  await waitFor(()=>expect(vi.mocked(rpc).mock.calls.some(([m,p]:any)=>m==="media.list"&&p.chatId==="A"&&p.topicId==="5")).toBe(true));
  fireEvent.contextMenu(await screen.findByRole("button",{name:"预览 A-1.bin"}));
  fireEvent.click(screen.getByRole("menuitem",{name:"选择消息"}));
  fireEvent.click(screen.getByRole("button",{name:"全部聊天"}));
  expect(screen.getByRole("button",{name:"预览 A-1.bin"})).toBeTruthy();
  expect(screen.getByText("已选 1 项")).toBeTruthy();
  fireEvent.click(screen.getByText("下载所选"));
  await screen.findByText("下载清单");
  expect(vi.mocked(rpc).mock.calls.find(([m])=>m==="media.previewSelection")?.[1]).toMatchObject({rule:{chatId:"A",topicId:"5"},messageIds:["1"]});
});

it("refreshes archive membership without overriding the selected folder",async()=>{
  let resolveRefresh!: (value: unknown)=>void;
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="chats.list")return archivedChats;
    if(method==="chats.refresh")return new Promise(resolve=>{resolveRefresh=resolve;});
    if(method==="chats.folders")return {folders:archiveFolders,selectedFolderId:"all"};
    return base(method,p);
  });
  render(<App/>);await screen.findByText("测试聊天B");
  await waitFor(()=>expect(resolveRefresh).toBeTypeOf("function"));
  fireEvent.click(screen.getByRole("button",{name:"已归档"}));
  await act(async()=>resolveRefresh([{...chats[0],archived:false},{...chats[1],archived:true}]));
  await screen.findByText("测试聊天B");
  expect(screen.queryByText("测试聊天A")).toBeNull();
  expect(screen.getByRole("button",{name:"已归档"}).getAttribute("aria-pressed")).toBe("true");
});

it("isolates saved archive choices and discards a previous account's late refresh",async()=>{
  const second={id:"b",namespace:"b",displayName:"第二账户",active:false};
  let active=boot.activeAccount;
  let resolveA!: (value:unknown)=>void;
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="app.bootstrap")return {...boot,accounts:[...boot.accounts,second],activeAccount:active};
    if(method==="accounts.use"){active=second;return true;}
    if(method==="chats.list")return p.accountId==="a"?archivedChats:[{...chats[1],accountId:"b"}];
    if(method==="chats.refresh")return p.accountId==="a"?new Promise(resolve=>{resolveA=resolve;}):[{...chats[1],accountId:"b"}];
    if(method==="chats.folders")return {folders:archiveFolders,selectedFolderId:p.accountId==="a"?"archived":"all"};
    return base(method,p);
  });
  render(<App/>);await screen.findByText("测试聊天A");
  await waitFor(()=>expect(resolveA).toBeTypeOf("function"));
  fireEvent.change(screen.getByLabelText("当前账户"),{target:{value:"b"}});
  await screen.findByText("测试聊天B");
  await act(async()=>resolveA(archivedChats));
  expect(screen.queryByText("测试聊天A")).toBeNull();
  expect(screen.getByRole("button",{name:"全部聊天"}).getAttribute("aria-pressed")).toBe("true");
  fireEvent.click(screen.getByRole("button",{name:"已归档"}));
  expect(screen.getByText("暂无已归档对话")).toBeTruthy();
  expect(vi.mocked(rpc).mock.calls.some(([m,p]:any)=>m==="config.set"&&p.key==="ui.folder.b"&&p.value==="archived")).toBe(true);
});

it("shows the archive empty state even for an empty account and searches only within it",async()=>{
  vi.mocked(rpc).mockImplementation(async(method,p:any)=>{
    if(method==="chats.list"||method==="chats.refresh")return [];
    if(method==="chats.folders")return {folders:[],selectedFolderId:"archived"};
    return base(method,p);
  });
  render(<App/>);await screen.findByText("暂无已归档对话");
  expect(screen.queryByText("刷新以载入聊天列表")).toBeNull();
  fireEvent.change(screen.getByPlaceholderText("搜索聊天"),{target:{value:"missing"}});
  expect(screen.getByText("没有匹配的聊天")).toBeTruthy();
});
