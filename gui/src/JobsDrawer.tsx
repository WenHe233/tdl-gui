import { useEffect, useRef, useState } from "react";
import { openPath, revealItemInDir } from "@tauri-apps/plugin-opener";
import { X } from "lucide-react";
import { onWorkerEvent, rpc } from "./rpc";
import { formatBytes, speedLabel, speedHint } from "./browsing";
import type { Job, JobItem } from "./types";

const states: Record<string, string> = {
  queued: "等待下载",
  running: "下载中",
  paused: "已暂停",
  failed: "失败",
  completed: "已完成",
  cancelled: "已取消",
  done: "已完成",
  downloading: "下载中",
  verifying: "校验中",
};
export function JobsDrawer({
  jobs,
  onClose,
  onError,
}: {
  jobs: Job[];
  onClose: () => void;
  onError: (s: string) => void;
}) {
  const [expanded, setExpanded] = useState<string>();
  const [items, setItems] = useState<JobItem[]>([]);
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const locks = useRef(new Set<string>());
  const request = useRef(0);
  useEffect(() => {
    const token = ++request.current;
    setItems([]);
    if (!expanded) return;
    const load = () =>
      rpc<{ items: JobItem[] }>("jobs.get", { id: expanded })
        .then((x) => {
          if (request.current === token) setItems(x.items);
        })
        .catch((e) => {
          if (request.current === token) onError(String(e));
        });
    void load();
    const off = onWorkerEvent((e) => {
      if (e.jobId === expanded && e.type === "job.progress" && e.items) {
        const updates = new Map(e.items.map((it) => [it.messageId, it]));
        setItems((previous) => previous.map((it) => updates.get(it.messageId) || it));
        return;
      }
      if (
        e.jobId === expanded &&
        (e.type === "item.updated" || e.type === "job.updated")
      )
        void load();
    });
    return () => {
      request.current++;
      off();
    };
  }, [expanded]);
  const act = async (j: Job, method: string) => {
    if (locks.current.has(j.id)) return;
    locks.current.add(j.id);
    setBusy(new Set(locks.current));
    try {
      await rpc(method, { id: j.id });
    } catch (e) {
      onError(String(e));
    } finally {
      locks.current.delete(j.id);
      setBusy(new Set(locks.current));
    }
  };
  const openFile = async (it: JobItem, reveal: boolean) => {
    try {
      if (reveal) await revealItemInDir(it.targetPath);
      else await openPath(it.targetPath);
    } catch (e) {
      onError(String(e));
    }
  };
  return (
    <div className="drawer-backdrop">
      <aside className="drawer jobs-drawer">
        <header>
          <div>
            <h2>下载任务</h2>
            <p>查看文件结果、恢复或重试</p>
            <span className="speed" title={speedHint}>总下载速度 {speedLabel(jobs.filter((j) => j.state === "running").reduce((n,j) => n+(j.speedBytesPerSecond || 0),0))}</span>
          </div>
          <button onClick={onClose} aria-label="关闭任务">
            <X />
          </button>
        </header>
        <div className="job-list">
          {jobs.map((j) => (
            <article key={j.id}>
              <div className="job-top">
                <span className={`state ${j.state}`}>
                  {states[j.state] || j.state}
                </span>
                <strong>
                  {j.doneFiles}/{j.totalFiles} 个文件
                </strong>
                <small>失败 {j.failedFiles} 项</small>
              </div>
              <div className="progress">
                <i
                  style={{
                    width: `${j.totalBytes ? Math.min(100, Math.round((j.doneBytes / j.totalBytes) * 100)) : 0}%`,
                  }}
                />
              </div>
              <div className="job-speed"><span className="speed">{formatBytes(j.doneBytes)} / {formatBytes(j.totalBytes)}</span><span className="speed" title={speedHint}>{speedLabel(j.state === "running" ? j.speedBytesPerSecond : 0)}</span></div>
              {j.error && <p>{j.error}</p>}
              <div className="job-actions">
                <button
                  onClick={() =>
                    setExpanded(expanded === j.id ? undefined : j.id)
                  }
                >
                  {expanded === j.id ? "收起详情" : "文件详情"}
                </button>
                {j.state === "running" ? (
                  <button
                    disabled={busy.has(j.id)}
                    onClick={() => act(j, "jobs.pause")}
                  >
                    暂停
                  </button>
                ) : (
                  !["cancelled"].includes(j.state) && (
                    <button
                      disabled={busy.has(j.id)}
                      onClick={() => act(j, "jobs.start")}
                    >
                      {j.state === "completed" ? "检查并恢复缺失文件" : "恢复"}
                    </button>
                  )
                )}
                {j.failedFiles > 0 &&
                  j.state !== "running" &&
                  j.state !== "cancelled" && (
                    <button
                      disabled={busy.has(j.id)}
                      onClick={() => act(j, "jobs.retry")}
                    >
                      重试失败项
                    </button>
                  )}
                {!["completed", "cancelled"].includes(j.state) && (
                  <button
                    disabled={busy.has(j.id)}
                    onClick={() => act(j, "jobs.cancel")}
                  >
                    取消任务
                  </button>
                )}
              </div>
              {expanded === j.id && (
                <div className="job-details">
                  {items.map((it) => (
                    <div key={it.messageId}>
                      <strong>{it.targetPath.split(/[\\/]/).pop()}</strong>
                      <span>{states[it.state] || it.state}</span>
                      <div className="file-progress"><span>{formatBytes(it.state === "done" ? it.size : it.downloadedBytes || 0)} / {formatBytes(it.size)}</span><span title={speedHint}>{speedLabel(j.state === "running" ? it.speedBytesPerSecond : 0)}</span></div>
                      <small>{it.targetPath}</small>
                      {it.error && <p>{it.error}</p>}
                      {it.state === "done" && (
                        <div>
                          <button onClick={() => openFile(it, true)}>
                            打开所在目录
                          </button>
                          <button onClick={() => openFile(it, false)}>
                            打开文件
                          </button>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </article>
          ))}
          {!jobs.length && <p className="empty-state">还没有下载任务</p>}
        </div>
      </aside>
    </div>
  );
}
