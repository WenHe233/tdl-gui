import { useEffect, useRef, useState } from "react";
import { Download, Network, HardDrive, Cpu, RefreshCw, X, FolderOpen, Check } from "lucide-react";
import { open } from "@tauri-apps/plugin-dialog";
import { rpc } from "./rpc";
import { UpdatePanel } from "./UpdatePanel";
import type { Bootstrap, Engine, Settings } from "./types";
import "./settings.css";

const sections = [
  { id: "download", title: "下载", icon: Download, hint: "调整并发与重试" },
  { id: "network", title: "网络", icon: Network, hint: "代理与连接" },
  { id: "storage", title: "存储", icon: HardDrive, hint: "目录与预览缓存" },
  { id: "engine", title: "引擎", icon: Cpu, hint: "管理 tdl" },
  { id: "updates", title: "应用更新", icon: RefreshCw, hint: "版本与更新记录" },
] as const;
type Section = typeof sections[number]["id"];
type Draft = Record<string, string>;
const MiB = 1024 * 1024;
const recommended: Draft = { "file.threads": "8", "file.concurrency": "4", "pool.size": "8", "task.delay": "0s", retries: "3" };
function fromSettings(s: Settings): Draft {
  return { "file.threads": String(s.fileThreads ?? 8), "file.concurrency": String(s.fileConcurrency), "pool.size": String(s.poolSize ?? 8), "task.delay": s.taskDelay ?? "0s", retries: String(s.retries), proxy: s.proxy ?? "", ntp: s.ntp ?? "", "reconnect.timeout": s.reconnectTimeout ?? "5m", "download.root": s.downloadRoot, "min.free.bytes": String((s.minFreeBytes ?? 1024*MiB)/MiB), "cache.max.bytes": String(s.cacheMaxBytes/MiB) };
}
const fieldSections: Record<string, Section> = { "file.threads": "download", "file.concurrency": "download", "pool.size": "download", "task.delay": "download", retries: "download", proxy: "network", ntp: "network", "reconnect.timeout": "network", "download.root": "storage", "min.free.bytes": "storage", "cache.max.bytes": "storage" };
export function SettingsModal({ settings, engine, version, updateResult, onUpdate, onClose, onInstall, onChoose, onClear, onSaved }: {
  settings: Settings; engine?: Engine; version: string; updateResult: Bootstrap["updateResult"];
  onUpdate: () => Promise<void>; onClose: () => void; onInstall: () => Promise<void>;
  onChoose: () => Promise<void>; onClear: () => Promise<void>; onSaved: (s: Settings) => void;
}) {
  const [section, setSection] = useState<Section>("download");
  const [baseline, setBaseline] = useState(() => fromSettings(settings));
  const [draft, setDraft] = useState(baseline);
  const [errors, setErrors] = useState<Draft>({});
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);
  const lock = useRef(false);
  const dialog = useRef<HTMLDivElement>(null);
  const confirm = useRef<HTMLDivElement>(null);
  const dirty = Object.keys(draft).some((k) => draft[k] !== baseline[k]);
  const requestClose = () => { if (!lock.current) { if (dirty) setConfirmClose(true); else onClose(); } };
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    dialog.current?.querySelector<HTMLButtonElement>("nav button")?.focus();
    return () => previous?.focus();
  }, []);
  useEffect(() => {
    if (confirmClose) {
      confirm.current?.querySelector<HTMLButtonElement>("button")?.focus();
      return () => dialog.current?.querySelector<HTMLButtonElement>("nav button")?.focus();
    }
  }, [confirmClose]);
  useEffect(() => {
    const key = Object.keys(errors).find((key) => errors[key] && fieldSections[key]);
    if (key) document.getElementById(`setting-${key}`)?.focus();
  }, [errors]);
  const change = (key: string, value: string) => {
    setDraft((v) => ({ ...v, [key]: value })); setErrors((v) => ({ ...v, [key]: "", general: "" })); setMessage("");
  };
  const save = async (close = false) => {
    if (lock.current) return;
    const values: Draft = {}, problems: Draft = {};
    for (const [key, value] of Object.entries(draft)) {
      if (value === baseline[key]) continue;
      if (["file.threads", "file.concurrency", "pool.size", "retries"].includes(key)) {
        const min = key === "file.threads" || key === "file.concurrency" ? 1 : 0;
        if (!/^\d+$/.test(value) || Number(value) < min || Number(value) > 2147483647) problems[key] = `请输入不小于 ${min} 的整数`;
      }
      if (key === "cache.max.bytes" || key === "min.free.bytes") {
        const n = Number(value) * MiB;
        if (!value.trim() || !Number.isSafeInteger(n) || n < 0) problems[key] = "请输入有效的非负容量";
        else values[key] = String(n);
      } else values[key] = value;
    }
    if (Object.keys(problems).length) {
      setErrors(problems); setSection(fieldSections[Object.keys(problems)[0]]); setConfirmClose(false); return;
    }
    lock.current = true; setBusy(true); setErrors({}); setMessage("");
    try {
      const saved = await rpc<Settings>("config.update", { values });
      const next = fromSettings(saved);
      setDraft(next); setBaseline(next); onSaved(saved); setMessage("已保存。后续启动或恢复的下载使用新参数。");
      setConfirmClose(false); if (close) onClose();
    } catch (e) {
      const text = String(e).replace(/^Error: /, "");
      const key = Object.keys(fieldSections).find((key) => text.startsWith(`${key}:`));
      setErrors(key ? { [key]: text.slice(key.length + 1).trim() } : { general: text });
      if (key) setSection(fieldSections[key]);
      setConfirmClose(false);
    } finally { lock.current = false; setBusy(false); }
  };
  const action = async (run: () => Promise<void>, success = "") => {
    if (lock.current) return;
    lock.current = true; setBusy(true); setErrors({}); setMessage("");
    try { await run(); setMessage(success); } catch(e) { setErrors({ general: String(e) }); }
    finally { lock.current = false; setBusy(false); }
  };
  const field = (key: string, label: string, help: string, type = "number", min = 0) => (
    <div className="settings-field" key={key}>
      <label htmlFor={`setting-${key}`}>{label}</label>
      <input id={`setting-${key}`} type={type} min={type === "number" ? min : undefined} step={key.endsWith(".bytes") ? "any" : "1"} value={draft[key]} disabled={busy} onChange={(e) => change(key, e.target.value)} aria-invalid={!!errors[key]} aria-describedby={`hint-${key}`} autoComplete="off" spellCheck={false}/>
      <small id={`hint-${key}`} className={errors[key] ? "settings-field-error" : ""}>{errors[key] || help}</small>
    </div>
  );
  const title = sections.find((s) => s.id === section)!;
  return <div className="modal-backdrop settings-backdrop">
    <div className="settings-shell" ref={dialog} role="dialog" aria-modal="true" aria-labelledby="settings-heading" onKeyDown={(e) => {
      if (e.key === "Escape") { e.stopPropagation(); if (confirmClose) setConfirmClose(false); else requestClose(); }
      if (e.key === "Tab") {
        const root = confirmClose ? confirm.current : dialog.current;
        const targets = Array.from(root?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),[tabindex="0"]') || []).filter((el) => !el.closest('[hidden]'));
        const first = targets[0], last = targets[targets.length-1];
        if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
      }
    }}>
      <header className="settings-header"><div><h2 id="settings-heading">设置</h2><p>下载、连接与应用管理</p></div><button className="icon-button" aria-label="关闭设置" disabled={busy} onClick={requestClose}><X size={20}/></button></header>
      <div className="settings-body">
        <nav aria-label="设置分类">{sections.map(({id, title, icon: Icon, hint}) => <button key={id} aria-current={section === id ? "page" : undefined} onClick={() => {setSection(id);setMessage("");}}><Icon size={19}/><span><strong>{title}</strong><small>{hint}</small></span></button>)}<div className="settings-version">TDL Media<br/><span>{version}</span></div></nav>
        <main className="settings-content">
          <div className="settings-section-title"><h3>{title.title}</h3><p>{section === "download" ? "参数在下次启动或恢复下载时生效，当前任务继续使用原配置。" : section === "network" ? "保存后用于新建连接，正在运行的操作继续使用原配置。" : section === "storage" ? "管理文件保存位置和预览缓存。" : section === "engine" ? "下载由官方 tdl 引擎执行。" : "查看当前版本并获取正式更新。"}</p></div>
          {section === "download" && <>
            <div className="settings-recommendation"><div><strong>推荐下载配置</strong><p>8 线程 / 4 个文件 / 连接池 8</p></div><button disabled={busy} onClick={() => {setDraft((v) => ({...v, ...recommended}));setErrors({});setMessage("推荐参数已填入，保存后生效。");}}>恢复推荐参数</button></div>
            <div className="settings-grid">{field("file.threads", "单文件线程数", "同时请求一个文件的多个分块。", "number", 1)}{field("file.concurrency", "同时下载文件数", "一个下载任务内同时传输的文件数。", "number", 1)}{field("pool.size", "连接池大小", "每个数据中心的连接池大小；0 为不限制。")}{field("retries", "失败重试次数", "首次下载失败后的重试次数；0 为不重试。")}{field("task.delay", "文件启动间隔", "支持 0s、500ms、2s；0s 为无间隔。", "text")}</div>
            <p className="settings-note">并发越高不一定越快，实际速度受网络、代理和 Telegram 影响。速度显示按最近 2 秒的文件增长量估算。</p>
          </>}
          {section === "network" && <div className="settings-grid single">{field("proxy", "代理地址", "HTTP、HTTPS 或 SOCKS5 地址，例如 socks5://127.0.0.1:1080；留空为直连。", "text")}{field("ntp", "NTP 服务器", "填写主机名；留空时 tdl 使用系统时间，内置客户端通过 HTTPS 校时。", "text")}{field("reconnect.timeout", "重连超时", "例如 5m、30s；0s 为不限制。", "text")}</div>}
          {section === "storage" && <>
            <div className="settings-path">{field("download.root", "默认下载目录", "用于新建清单，已有清单的目标路径保持不变。", "text")}<button aria-label="选择下载目录" disabled={busy} onClick={() => void action(async () => {const result = await open({ directory: true, multiple: false });if (typeof result === "string") change("download.root", result);})}><FolderOpen size={18}/></button></div>
            <div className="settings-grid">{field("min.free.bytes", "最低剩余空间（MiB）", "下载前检查目标磁盘的剩余空间。")}{field("cache.max.bytes", "预览缓存上限（MiB）", "超出后清理较早的媒体预览；0 为不限制。")}</div>
            <div className="settings-info"><span>数据目录</span><code>{settings.dataDir}</code><small>账户、索引和任务记录保存在此处。</small></div>
            <button className="settings-secondary" disabled={busy} onClick={() => void action(onClear, "预览缓存已清理。")}>清理预览缓存</button>
          </>}
          {section === "engine" && <><div className="settings-info"><span>当前引擎</span><strong>{engine?.version || "尚未安装"}</strong><code>{engine?.path || "安装引擎后即可下载媒体。"}</code></div><div className="settings-actions"><button disabled={busy} onClick={() => void action(onChoose)}>指定 tdl.exe</button><button className="primary" disabled={busy} onClick={() => void action(onInstall)}>安装 / 更新 v0.20.4</button></div></>}
          <div hidden={section !== "updates"}>{dirty && <p className="settings-note">安装更新前请先保存设置修改。</p>}<UpdatePanel version={version} result={updateResult} onApply={onUpdate} disabled={busy || dirty} onBusyChange={(value) => { lock.current = value; setBusy(value); }}/></div>
          {errors.general && <p className="inline-error" role="alert">{errors.general}</p>}
        </main>
      </div>
      <footer className="settings-footer"><div role="status">{busy ? "正在处理…" : message ? <><Check size={16}/>{message}</> : dirty ? "有未保存的修改" : "修改后点击保存"}</div><button className="primary" disabled={busy || !dirty} onClick={() => void save()}>保存设置</button></footer>
      {confirmClose && <div className="settings-confirm"><div ref={confirm} role="alertdialog" aria-modal="true" aria-labelledby="unsaved-heading"><h3 id="unsaved-heading">保存设置修改？</h3><p>关闭前可以保存修改，或放弃本次编辑。</p><div className="settings-actions"><button disabled={busy} onClick={() => {setConfirmClose(false);dialog.current?.querySelector<HTMLButtonElement>("nav button")?.focus();}}>继续编辑</button><button disabled={busy} onClick={onClose}>放弃修改</button><button className="primary" disabled={busy} onClick={() => void save(true)}>保存并关闭</button></div></div></div>}
    </div>
  </div>;
}
