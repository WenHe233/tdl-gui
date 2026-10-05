import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { WorkerEvent } from "./types";

type Pending = {
  resolve: (value: unknown) => void;
  reject: (reason: Error) => void;
};
const pending = new Map<number, Pending>();
let id = 1;
let ready: Promise<void> | null = null;
let listeners: Promise<void> | null = null;
const eventListeners = new Set<(event: WorkerEvent) => void>();

export function startWorker(): Promise<void> {
  if (ready) return ready;
  ready = (async () => {
    if (!listeners)
      listeners = (async () => {
        await listen<string>("worker-message", ({ payload }) => {
          let msg;
          try {
            msg = JSON.parse(payload);
          } catch {
            return;
          }
          if (msg.method === "event") {
            eventListeners.forEach((fn) => fn(msg.params));
            return;
          }
          const target = pending.get(msg.id);
          if (!target) return;
          pending.delete(msg.id);
          if (msg.error) target.reject(new Error(msg.error.message));
          else target.resolve(msg.result);
        });
        await listen("worker-stopped", () => {
          pending.forEach((p) =>
            p.reject(new Error("下载后台已退出，请重新启动应用")),
          );
          pending.clear();
          ready = null;
        });
      })();
    await listeners;
    await invoke("worker_start");
  })().catch((e) => {
    ready = null;
    throw e;
  });
  return ready;
}

export async function rpc<T>(method: string, params: unknown = {}): Promise<T> {
  await startWorker();
  const requestId = id++;
  return new Promise<T>((resolve, reject) => {
    pending.set(requestId, { resolve: (value) => resolve(value as T), reject });
    void invoke("worker_send", {
      line: JSON.stringify({ jsonrpc: "2.0", id: requestId, method, params }),
    }).catch((e) => {
      pending.delete(requestId);
      reject(e);
    });
  });
}
export function onWorkerEvent(fn: (event: WorkerEvent) => void) {
  eventListeners.add(fn);
  return () => {
    eventListeners.delete(fn);
  };
}
