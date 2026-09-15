import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { WorkerEvent } from "./types";

type Pending = { resolve:(value:unknown)=>void; reject:(reason:Error)=>void };
const pending = new Map<number, Pending>();
let id = 1;
let ready: Promise<void> | null = null;
const eventListeners = new Set<(event:WorkerEvent)=>void>();

export function startWorker():Promise<void>{
  if (ready) return ready;
  ready = (async()=>{
    await listen<string>("worker-message", ({payload})=>{
      const msg = JSON.parse(payload);
      if (msg.method === "event") { eventListeners.forEach(fn=>fn(msg.params)); return; }
      const target = pending.get(msg.id);
      if (!target) return;
      pending.delete(msg.id);
      if (msg.error) target.reject(new Error(msg.error.message)); else target.resolve(msg.result);
    });
    await invoke("worker_start");
  })();
  return ready;
}

export async function rpc<T>(method:string, params:unknown={}):Promise<T>{
  await startWorker();
  const requestId = id++;
  const promise = new Promise<T>((resolve,reject)=>pending.set(requestId,{resolve:value=>resolve(value as T),reject}));
  await invoke("worker_send",{line:JSON.stringify({jsonrpc:"2.0",id:requestId,method,params})});
  return promise;
}
export function onWorkerEvent(fn:(event:WorkerEvent)=>void){eventListeners.add(fn);return()=>eventListeners.delete(fn)}
