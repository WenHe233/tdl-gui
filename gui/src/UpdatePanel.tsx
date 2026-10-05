import { useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { rpc } from "./rpc";
import type { AppUpdate, Bootstrap } from "./types";

export function UpdatePanel({
  version,
  result,
  onApply,
}: {
  version: string;
  result: Bootstrap["updateResult"];
  onApply: () => Promise<void>;
}) {
  const [info, setInfo] = useState<AppUpdate>();
  const [phase, setPhase] = useState("");
  const [error, setError] = useState<string>();
  const lock = useRef(false);
  const check = async () => {
    if (lock.current) return;
    lock.current = true;
    setPhase("正在检测更新…");
    setError(undefined);
    try {
      setInfo(await rpc<AppUpdate>("app.update.check"));
    } catch (e) {
      setError(String(e));
    } finally {
      lock.current = false;
      setPhase("");
    }
  };
  const update = async () => {
    if (lock.current) return;
    lock.current = true;
    setError(undefined);
    setPhase("正在下载并校验更新包…");
    try {
      await rpc("app.update.prepare");
      const process = await invoke<{ pid: number; path: string }>(
        "app_process",
      );
      await rpc("app.update.apply", process);
      setPhase("正在保存进度，更新后将重新启动…");
      await onApply();
    } catch (e) {
      setError(String(e));
      setPhase("");
      lock.current = false;
    }
  };
  return (
    <section className="update-panel">
      <h3>应用更新</h3>
      <p>当前版本 {version}</p>
      {result && !result.success && (
        <p className="inline-error">上次更新失败：{result.error}</p>
      )}
      {info && (
        <p>
          {info.available ? `发现新版本 ${info.latest}` : "当前已是最新版本"}
        </p>
      )}
      {info?.available && (
        <>
          <p className="update-notes">
            {info.notes || "此版本未提供更新说明。"}
          </p>
          <p>更新会保存下载进度并重启应用。账户、规则和下载文件会保留。</p>
        </>
      )}
      {phase && <p role="status">{phase}</p>}
      {error && (
        <p role="alert" className="inline-error">
          {error}
        </p>
      )}
      <div className="modal-actions">
        <button disabled={!!phase} onClick={check}>
          检测更新
        </button>
        {info?.available && (
          <button className="primary" disabled={!!phase} onClick={update}>
            一键更新并重启
          </button>
        )}
      </div>
    </section>
  );
}
