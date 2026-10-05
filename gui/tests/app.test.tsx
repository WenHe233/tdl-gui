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
