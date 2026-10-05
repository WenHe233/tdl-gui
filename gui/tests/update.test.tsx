import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { UpdatePanel } from "../src/UpdatePanel";
import { rpc } from "../src/rpc";
import { invoke } from "@tauri-apps/api/core";
vi.mock("../src/rpc", () => ({ rpc: vi.fn() }));
vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
beforeEach(() => vi.clearAllMocks());
it("checks then downloads, schedules, and shuts down in order", async () => {
  const calls: string[] = [];
  vi.mocked(rpc).mockImplementation(async (method) => {
    calls.push(method);
    return {
      available: true,
      current: "0.2.0",
      latest: "0.2.1",
      notes: "修复下载",
      url: "https://github.com/WenHe233/tdl-gui",
    };
  });
  vi.mocked(invoke).mockResolvedValue({ pid: 1, path: "app.exe" });
  const apply = vi.fn(async () => {
    calls.push("shutdown");
  });
  render(<UpdatePanel version="0.2.0" result={undefined} onApply={apply} />);
  fireEvent.click(screen.getByText("检测更新"));
  await screen.findByText("发现新版本 0.2.1");
  fireEvent.click(screen.getByText("一键更新并重启"));
  fireEvent.click(screen.getByText("一键更新并重启"));
  await waitFor(() => expect(apply).toHaveBeenCalledOnce());
  expect(calls).toEqual([
    "app.update.check",
    "app.update.prepare",
    "app.update.apply",
    "shutdown",
  ]);
});
it("keeps the app open when verification fails and allows retry", async () => {
  vi.mocked(rpc).mockImplementation(async (method) => {
    if (method === "app.update.prepare") throw new Error("SHA256 校验失败");
    return { available: true, latest: "0.2.1", notes: "" };
  });
  const apply = vi.fn();
  render(<UpdatePanel version="0.2.0" result={undefined} onApply={apply} />);
  fireEvent.click(screen.getByText("检测更新"));
  await screen.findByText("发现新版本 0.2.1");
  fireEvent.click(screen.getByText("一键更新并重启"));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("SHA256"),
  );
  expect(apply).not.toHaveBeenCalled();
  expect(invoke).not.toHaveBeenCalled();
  expect(screen.getByText("一键更新并重启")).toHaveProperty("disabled", false);
});
