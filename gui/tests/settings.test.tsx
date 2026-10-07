import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { SettingsModal } from "../src/SettingsModal";
import { rpc } from "../src/rpc";
import type { Settings } from "../src/types";
vi.mock("../src/rpc", () => ({ rpc: vi.fn() }));
vi.mock("@tauri-apps/plugin-dialog", () => ({ open: vi.fn() }));
vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
const settings: Settings = { dataDir: "C:/Data", downloadRoot: "D:/Downloads", fileThreads: 8, fileConcurrency: 4, poolSize: 8, taskDelay: "0s", reconnectTimeout: "5m", proxy: "", ntp: "", retries: 3, minFreeBytes: 1073741824, cacheMaxBytes: 524288000 };
const onSaved = vi.fn(), onClose = vi.fn();
function show() { render(<SettingsModal settings={settings} version="0.3.4" updateResult={undefined} onUpdate={async()=>{}} onClose={onClose} onSaved={onSaved} onInstall={async()=>{}} onChoose={async()=>{}} onClear={async()=>{}}/>); }
function section(name: string) { fireEvent.click(screen.getByRole("button",{name:new RegExp(`^${name}`)})); }
beforeEach(() => { vi.clearAllMocks(); vi.mocked(rpc).mockResolvedValue(settings); });
it("keeps changes across categories and submits one atomic patch", async () => {
 show(); fireEvent.change(screen.getByLabelText("单文件线程数"),{target:{value:"12"}});
 section("网络"); fireEvent.change(screen.getByLabelText("代理地址"),{target:{value:"socks5://localhost:1080"}});
 section("存储"); fireEvent.change(screen.getByLabelText("预览缓存上限（MiB）"),{target:{value:"750"}});
 section("下载"); expect((screen.getByLabelText("单文件线程数") as HTMLInputElement).value).toBe("12");
 fireEvent.click(screen.getByRole("button",{name:"保存设置"}));
 await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
 expect(rpc).toHaveBeenCalledWith("config.update",{values:{"file.threads":"12",proxy:"socks5://localhost:1080","cache.max.bytes":"786432000"}});
 expect((screen.getByRole("button",{name:"保存设置"}) as HTMLButtonElement).disabled).toBe(true);
});
it("rejects invalid numbers and places backend validation at the field",async()=>{
 show(); fireEvent.change(screen.getByLabelText("单文件线程数"),{target:{value:"0"}});
 fireEvent.click(screen.getByRole("button",{name:"保存设置"})); expect(rpc).not.toHaveBeenCalled(); expect(screen.getByText("请输入不小于 1 的整数")).toBeTruthy();
 fireEvent.change(screen.getByLabelText("单文件线程数"),{target:{value:"12"}});
 vi.mocked(rpc).mockRejectedValueOnce(new Error("task.delay: 请输入非负时长"));
 fireEvent.click(screen.getByRole("button",{name:"保存设置"}));
 await screen.findByText("请输入非负时长"); expect(screen.getByLabelText("文件启动间隔").getAttribute("aria-invalid")).toBe("true");expect(onSaved).not.toHaveBeenCalled();
});
it("restores recommended parameters and handles unsaved closing",()=>{
 show(); fireEvent.change(screen.getByLabelText("单文件线程数"),{target:{value:"2"}});
 fireEvent.click(screen.getByRole("button",{name:"关闭设置"})); expect(screen.getByRole("alertdialog")).toBeTruthy();expect(onClose).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"继续编辑"}));
 fireEvent.click(screen.getByRole("button",{name:"恢复推荐参数"}));expect((screen.getByLabelText("单文件线程数") as HTMLInputElement).value).toBe("8");
 fireEvent.change(screen.getByLabelText("同时下载文件数"),{target:{value:"2"}});
 fireEvent.click(screen.getByRole("button",{name:"关闭设置"}));fireEvent.click(screen.getByRole("button",{name:"放弃修改"}));expect(onClose).toHaveBeenCalledOnce();expect(rpc).not.toHaveBeenCalled();
});
it("prevents duplicate saves and closes only after successful save",async()=>{
 let resolve!: (s:Settings)=>void;vi.mocked(rpc).mockReturnValueOnce(new Promise<Settings>(r=>resolve=r));
 show();fireEvent.change(screen.getByLabelText("单文件线程数"),{target:{value:"4"}});
 fireEvent.click(screen.getByRole("button",{name:"关闭设置"}));const button=screen.getByRole("button",{name:"保存并关闭"});fireEvent.click(button);fireEvent.click(button);expect(rpc).toHaveBeenCalledOnce();expect(onClose).not.toHaveBeenCalled();
 resolve({...settings,fileThreads:4});await waitFor(()=>expect(onClose).toHaveBeenCalledOnce());
});
it("keeps drafts after storage failure and supports Escape",async()=>{
 vi.mocked(rpc).mockRejectedValueOnce(new Error("保存失败"));show();fireEvent.change(screen.getByLabelText("单文件线程数"),{target:{value:"4"}});fireEvent.click(screen.getByRole("button",{name:"保存设置"}));await screen.findByRole("alert");
 expect((screen.getByLabelText("单文件线程数") as HTMLInputElement).value).toBe("4");fireEvent.keyDown(screen.getByRole("dialog"),{key:"Escape"});expect(screen.getByRole("alertdialog")).toBeTruthy();
 fireEvent.keyDown(screen.getByRole("dialog"),{key:"Escape"});expect(screen.queryByRole("alertdialog")).toBeNull();expect(document.activeElement).toBe(screen.getByRole("button",{name:/^下载\s*调整并发与重试$/}));
});
