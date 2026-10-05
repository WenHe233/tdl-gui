import { useEffect, useMemo, useRef, useState } from "react";
import { invoke, convertFileSrc } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { open } from "@tauri-apps/plugin-dialog";
import { openPath } from "@tauri-apps/plugin-opener";
import {
  Archive,
  Check,
  ChevronDown,
  CircleAlert,
  Download,
  File,
  FileAudio,
  FileImage,
  Film,
  FolderOpen,
  Image,
  LoaderCircle,
  LogIn,
  Menu,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Search,
  Settings,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { onWorkerEvent, rpc, startWorker } from "./rpc";
import type {
  Account,
  Bootstrap,
  Chat,
  ChatFolder,
  DownloadPlan,
  Engine,
  Job,
  Media,
  Rule,
  WorkerEvent,
  MediaOperation,
} from "./types";
import "./avatar.css";
import { FolderRail } from "./FolderRail";
import { browseChats, speedLabel, speedHint } from "./browsing";
import { useSelection, RequestScope } from "./selection";
import { MediaCard } from "./MediaCard";
import { JobsDrawer } from "./JobsDrawer";
import { UpdatePanel } from "./UpdatePanel";

const DEFAULT_TEMPLATE =
  '{{.AccountName}}/{{.ChatName}}/{{date .Date "2006-01-02"}}-{{.MessageID}}-{{.OriginalName}}';
const kinds = [
  ["photo", "图片"],
  ["video", "视频"],
  ["audio", "音频"],
  ["voice", "语音"],
  ["document", "文件"],
  ["animation", "动图"],
  ["sticker", "贴纸"],
];
const kindIcon = (kind: string, size = 22) =>
  kind === "photo" ? (
    <FileImage size={size} />
  ) : kind === "video" || kind === "animation" ? (
    <Film size={size} />
  ) : kind === "audio" || kind === "voice" ? (
    <FileAudio size={size} />
  ) : (
    <File size={size} />
  );
const bytes = (n: number) => {
  if (!n) return "0 B";
  const u = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), u.length - 1);
  return `${(n / 1024 ** i).toFixed(i ? 1 : 0)} ${u[i]}`;
};
const dateInput = (v?: string) => {
  if (!v || v.startsWith("0001-")) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};
const dateBoundary = (v?: string, end = false) => {
  if (!v || v.startsWith("0001-")) return undefined;
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) {
    const [y, m, d] = v.split("-").map(Number);
    return new Date(
      y,
      m - 1,
      d,
      end ? 23 : 0,
      end ? 59 : 0,
      end ? 59 : 0,
      end ? 999 : 0,
    );
  }
  const parsed = new Date(v);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed;
};
const dateISO = (v?: string, end = false) =>
  dateBoundary(v, end)?.toISOString();
const normalizedExt = (m: Media) =>
  (m.extension || m.fileName.split(".").pop() || "")
    .replace(/^\./, "")
    .toLowerCase();
const hasExt = (items: string[] | undefined, ext: string) =>
  !!items?.some((item) => item.trim().replace(/^\./, "").toLowerCase() === ext);
const thumbnailExtensions = new Set([
  "jpg",
  "jpeg",
  "png",
  "webp",
  "gif",
  "bmp",
  "avif",
  "heic",
  "heif",
  "mp4",
  "mov",
  "m4v",
  "mkv",
  "webm",
  "avi",
  "wmv",
  "flv",
  "mpeg",
  "mpg",
  "3gp",
  "pdf",
]);
const canHaveThumbnail = (m: Media) =>
  ["photo", "video", "animation", "sticker"].includes(m.kind) ||
  thumbnailExtensions.has(normalizedExt(m));
const makeID = (prefix: string) =>
  `${prefix}_${crypto.randomUUID().replaceAll("-", "").slice(0, 16)}`;
const imageSource = (value: string) =>
  value.startsWith("data:") ? value : convertFileSrc(value);
const shutdownApp = async () => {
  try {
    await rpc("app.shutdown");
  } finally {
    try {
      await invoke("worker_stop");
    } finally {
      await invoke("quit_app");
    }
  }
};

type LoginState = {
  open: boolean;
  accountId?: string;
  proxy?: string;
  name: string;
  method: "qr" | "code" | "desktop";
  phone: string;
  desktopPath: string;
  desktopPasscode: string;
  loginId?: string;
  qrCode?: string;
  prompt?: string;
  inputKind?: string;
  choices?: string[];
  busy: boolean;
  error?: string;
};
const emptyLogin: LoginState = {
  open: false,
  name: "",
  method: "qr",
  phone: "",
  desktopPath: "",
  desktopPasscode: "",
  busy: false,
};

export default function App() {
  const [boot, setBoot] = useState<Bootstrap>();
  const [engine, setEngine] = useState<Engine>();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [active, setActive] = useState<Account>();
  const activeAccountId = useRef<string | undefined>(undefined);
  const [chats, setChats] = useState<Chat[]>([]);
  const [folders, setFolders] = useState<ChatFolder[]>([]);
  const [folderId, setFolderId] = useState("all");
  const [chatOrder, setChatOrder] = useState("recent");
  const [mediaOrder, setMediaOrder] = useState("newest");
  const [refreshingChats, setRefreshingChats] = useState(false);
  const [chatSyncError, setChatSyncError] = useState("");
  const chatGeneration = useRef(0);
  const streamRef = useRef<HTMLElement>(null);
  const chatListRef = useRef<HTMLDivElement>(null);
  const [removeAccount, setRemoveAccount] = useState<Account>();
  const [removingAccount, setRemovingAccount] = useState(false);
  const [scanError, setScanError] = useState("");
  const mediaOrderRef = useRef(mediaOrder);
  mediaOrderRef.current = mediaOrder;
  const [avatars, setAvatars] = useState<Record<string, string>>({});
  const requestedAvatars = useRef(new Set<string>());
  const [chat, setChat] = useState<Chat>();
  const [media, setMedia] = useState<Media[]>([]);
  const [indexedMediaCount, setIndexedMediaCount] = useState(0);
  const removedAccountIds = useRef(new Set<string>());
  const [mediaPreview, setMediaPreview] = useState<Media>();
  const [mediaOffset, setMediaOffset] = useState(0);
  const [hasMoreMedia, setHasMoreMedia] = useState(false);
  const [jobs, setJobs] = useState<Job[]>([]);
  const runningJobsRef = useRef<Job[]>([]);
  const [query, setQuery] = useState("");
  const [chatWindowStart, setChatWindowStart] = useState(0);
  const [loading, setLoading] = useState("正在启动…");
  const [error, setError] = useState<string>();
  const [login, setLogin] = useState<LoginState>(emptyLogin);
  const loginRef = useRef(login); loginRef.current = login;
  const [plan, setPlan] = useState<DownloadPlan>();
  const [showPlan, setShowPlan] = useState(false);
  const [showJobs, setShowJobs] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [closeChoice, setCloseChoice] = useState(false);
  const [topicId, setTopicId] = useState("");
  const [rule, setRule] = useState<Partial<Rule>>({
    order: "oldest",
    kinds: [],
    template: DEFAULT_TEMPLATE,
    maxFiles: 0,
    maxTotalSize: 0,
    minFileSize: 0,
    maxFileSize: 0,
  });

  const scope = `${active?.id || ""}/${chat?.id || ""}/${topicId}`;
  const selection = useSelection(scope);
  const [selectionMode, setSelectionMode] = useState(false);
  const [contextMenu, setContextMenu] = useState<{
    media: Media;
    x: number;
    y: number;
  }>();
  const [timeConfirm, setTimeConfirm] = useState<{
    media: Media;
    rule: Rule;
  }>();
  useEffect(() => {
    setSelectionMode(false);
    setOnlySelected(false);
    setContextMenu(undefined);
    setTimeConfirm(undefined); setScanError("");
  }, [scope]);
  useEffect(() => {
    if (!contextMenu) return;
    const dismiss = () => setContextMenu(undefined);
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") dismiss();
    };
    window.addEventListener("click", dismiss);
    window.addEventListener("keydown", key);
    window.addEventListener("resize", dismiss);
    return () => {
      window.removeEventListener("click", dismiss);
      window.removeEventListener("keydown", key);
      window.removeEventListener("resize", dismiss);
    };
  }, [contextMenu]);
  const [downloadedFilter, setDownloadedFilter] = useState("all");
  const [onlySelected, setOnlySelected] = useState(false);
  const [operation, setOperation] = useState<{
    id: string;
    scope: string;
    chat: Chat;
    topic: string;
    autoDownload: boolean;
  }>();
  const operationStarting = useRef(false);
  const [actionBusy, setActionBusy] = useState(false);
  const actionLock = useRef(false);
  const browserRequests = useRef(new RequestScope());
  const browserScope = useRef("");
  const pagePending = useRef(new Set<string>());

  const refreshBootstrap = async () => {
    const b = await rpc<Bootstrap>("app.bootstrap");
    setBoot(b);
 setChatOrder(b.settings.chatOrder || "recent");
 setMediaOrder(b.settings.mediaOrder || "newest");
    setEngine(b.engine);
    setAccounts(b.accounts);
    setActive(b.activeAccount);
    setJobs(b.jobs);
    return b;
  };
  const loadChats = async (accountId: string, generation: number, refresh = false) => {
    const current = () => activeAccountId.current === accountId && chatGeneration.current === generation;
    try {
      const list = await rpc<Chat[]>(refresh ? "chats.refresh" : "chats.list", { accountId, query: "" });
      if (!current()) return;
      if (Array.isArray(list)) setChats(list);
      const data = await rpc<{folders: ChatFolder[]; selectedFolderId?: string}>("chats.folders", {accountId});
      if (!current()) return;
      const next = Array.isArray(data?.folders) ? data.folders : [];
      setFolders(next);
      setFolderId((previous) => {const wanted = refresh ? previous : data?.selectedFolderId || "all"; return wanted === "all" || next.some((f) => f.id === wanted) ? wanted : "all";});
      if (refresh) setChatSyncError("");
    } catch (e) {
      if (current()) setChatSyncError(`聊天列表更新失败：${String(e)}`);
    } finally { if (refresh && current()) setRefreshingChats(false); }
  };
  const loadMedia = async (
    c: Chat,
    offset = 0,
    t = browserScope.current.startsWith(`${c.accountId}/${c.id}/`)
      ? topicId
      : "",
  ) => {
    const key = `${c.accountId}/${c.id}/${t}`;
    const displayOrder = mediaOrderRef.current;
    const token = browserRequests.current.enter(`${key}/${displayOrder}`, !offset);
    browserScope.current = key;
    const requestKey = `${token}/${offset}`;
    if (pagePending.current.has(requestKey)) return;
    pagePending.current.add(requestKey);
    setChat(c);
    setTopicId(t);
    if (!offset) {
      setMedia([]);
      setIndexedMediaCount(0);
      setHasMoreMedia(false);
      setMediaOffset(0);
      if (streamRef.current) streamRef.current.scrollTop = 0;
    }
    setLoading("正在载入媒体索引…");
    try {
      const page = await rpc<{
        items: Media[];
        indexedCount?: number;
        nextOffset: number;
        hasMore: boolean;
      }>("media.list", {
        accountId: c.accountId,
        chatId: c.id,
        topicId: t,
        offset,
        limit: 100,
        order: displayOrder,
        browseRule: rule.lastN ? {...rule, from: dateISO(rule.from), to: dateISO(rule.to, true)} : undefined,
      });
      if (!browserRequests.current.valid(token)) return;
      setMedia((v) =>
        offset
          ? [
              ...v,
              ...page.items.filter(
                (m) => !v.some((old) => old.messageId === m.messageId),
              ),
            ]
          : page.items,
      );
      setIndexedMediaCount(page.indexedCount ?? page.nextOffset);
      setMediaOffset(page.nextOffset);
      setHasMoreMedia(page.hasMore);
      const refs = page.items
        .filter((m) => canHaveThumbnail(m) && !m.thumbPath)
        .map((m) => ({ chatId: m.chatId, messageId: m.messageId }));
      if (refs.length)
        void rpc<Record<string, string>>("media.thumbnails", {
          accountId: c.accountId,
          items: refs,
        })
          .then((paths) => {
            if (browserRequests.current.valid(token))
              setMedia((v) =>
                v.map((m) =>
                  paths[m.messageId]
                    ? { ...m, thumbPath: paths[m.messageId] }
                    : m,
                ),
              );
          })
          .catch(() => {});
    } catch (e) {
      if (browserRequests.current.valid(token)) setError(String(e));
    } finally {
      pagePending.current.delete(requestKey);
      if (browserRequests.current.valid(token)) setLoading("");
    }
  };

  useEffect(() => {
    let disposed = false;
    let offEvent = () => {};
    let offClose = () => {};
    (async () => {
      try {
        await startWorker();
        if (disposed) return;
        offEvent = onWorkerEvent(handleEvent);
        const stop = await listen("app-close-requested", () => {
          if (runningJobsRef.current.length) setCloseChoice(true);
          else void shutdownApp();
        });
        if (disposed) {
          stop();
          return;
        }
        offClose = stop;
        await refreshBootstrap();
        if (!disposed) setLoading("");
      } catch (e) {
        if (!disposed) {
          setError(String(e));
          setLoading("");
        }
      }
    })();
    return () => {
      disposed = true;
      offEvent();
      offClose();
    };
  }, []);
  useEffect(() => {
    activeAccountId.current = active?.id;
    const generation = ++chatGeneration.current;
    setAvatars({}); requestedAvatars.current.clear(); setFolders([]); setFolderId("all");setChats([]);setChatSyncError("");
    if (active) {
      setRefreshingChats(true);
      void loadChats(active.id, generation).then(() => {if (chatGeneration.current === generation) void loadChats(active.id, generation, true);});
    } else setRefreshingChats(false);
  }, [active?.id]);
  useEffect(() => {
    runningJobsRef.current = jobs.filter((j) => j.state === "running");
  }, [jobs]);

  const handleEvent = (event: WorkerEvent) => {
    if (event.accountId && removedAccountIds.current.has(event.accountId)) return;
    if (event.type.startsWith("login.")) {
      if (!loginRef.current.open || (loginRef.current.loginId && event.loginId && loginRef.current.loginId !== event.loginId)) return;
      if (event.type === "login.completed" && event.account) {
        setLogin(emptyLogin);
        removedAccountIds.current.delete(event.account.id);
        setActive(event.account);
        refreshBootstrap();
        return;
      }
      if (event.type === "login.error") {
        setLogin((v) => ({
          ...v,
          busy: false,
          loginId: undefined,
          qrCode: undefined,
          inputKind: undefined,
          choices: undefined,
          error: event.error,
          prompt: "登录失败",
        }));
        return;
      }
      setLogin((v) => ({
        ...v,
        busy: false,
        loginId: event.loginId || v.loginId,
        qrCode: event.qrCode || v.qrCode,
        prompt: event.prompt,
        inputKind:
          event.type === "login.codeRequired"
            ? "code"
            : event.type === "login.passwordRequired"
              ? "password"
              : event.type === "login.desktopAccountRequired"
                ? "desktopAccount"
                : undefined,
        choices: event.choices,
      }));
    }
    if ((event.type === "job.updated" || event.type === "job.progress") && event.job)
      setJobs((v) => v.some((j) => j.id === event.job!.id) ? v.map((j) => j.id === event.job!.id ? event.job! : j) : [event.job!, ...v]);
    if (event.type === "item.updated" && event.item?.state === "done")
      setMedia((v) =>
        v.map((m) =>
          m.accountId === event.accountId &&
          m.chatId === event.item!.chatId &&
          (m.messageId === event.item!.messageId ||
            m.mediaId === event.item!.mediaId)
            ? { ...m, downloaded: true, localPath: event.item!.targetPath }
            : m,
        ),
      );
    if (event.type === "job.error") setError(event.message);
  };

  const filteredChats = useMemo(() => browseChats(chats, folders, folderId, chatOrder, query), [chats, folders, folderId, chatOrder, query]);
  useEffect(() => {setChatWindowStart(0); if (chatListRef.current) chatListRef.current.scrollTop = 0;}, [query, folderId, chatOrder]);
  useEffect(() => {
    if (!active) return;
    const ids = filteredChats
      .slice(chatWindowStart, chatWindowStart + 24)
      .map((c) => c.id)
      .filter((id) => !requestedAvatars.current.has(id));
    if (!ids.length) return;
    ids.forEach((id) => requestedAvatars.current.add(id));
    const accountId = active.id;
    rpc<Record<string, string>>("chats.avatars", { accountId, chatIds: ids })
      .then((found) => {
        if (activeAccountId.current === accountId)
          setAvatars((v) => ({ ...v, ...found }));
      })
      .catch(() => ids.forEach((id) => requestedAvatars.current.delete(id)));
  }, [active?.id, filteredChats, chatWindowStart]);
  const filteredMedia = useMemo(() => {
    const matches = media.filter((m) => {
      if (
        (downloadedFilter === "done" && !m.downloaded) ||
        (downloadedFilter === "pending" && m.downloaded)
      )
        return false;
      if (onlySelected && !selection.selected.has(m.messageId)) return false;
      if (rule.kinds?.length && !rule.kinds.includes(m.kind)) return false;
      if (rule.minFileSize && m.size < rule.minFileSize) return false;
      if (rule.maxFileSize && m.size > rule.maxFileSize) return false;
      const id = Number(m.messageId);
      if (rule.minMessageId && id < rule.minMessageId) return false;
      if (rule.maxMessageId && id > rule.maxMessageId) return false;
      const ext = normalizedExt(m);
      if (rule.includeExt?.length && !hasExt(rule.includeExt, ext))
        return false;
      if (hasExt(rule.excludeExt, ext)) return false;
      const hay = (m.fileName + "\n" + (m.caption || "")).toLowerCase();
      if (
        rule.includeKeyword &&
        !hay.includes(rule.includeKeyword.toLowerCase())
      )
        return false;
      if (
        rule.excludeKeyword &&
        hay.includes(rule.excludeKeyword.toLowerCase())
      )
        return false;
      const from = rule.recentDays
        ? new Date(new Date().setDate(new Date().getDate() - rule.recentDays))
        : dateBoundary(rule.from);
      const to = dateBoundary(rule.to, true);
      if (from && new Date(m.date) < from) return false;
      if (to && new Date(m.date) > to) return false;
      return true;
    });
    return matches;
  }, [media, rule, downloadedFilter, onlySelected, selection.selected]);
  const recentQuery = rule.lastN ? JSON.stringify([rule.lastN,rule.kinds,rule.from,rule.to,rule.recentDays,rule.minFileSize,rule.maxFileSize,rule.minMessageId,rule.maxMessageId,rule.includeExt,rule.excludeExt,rule.includeKeyword,rule.excludeKeyword]) : "";
  useEffect(() => {
    if (!chat) return;
    browserRequests.current.invalidate();
    const timer = setTimeout(() => void loadMedia(chat, 0, topicId), 150);
    return () => clearTimeout(timer);
  }, [mediaOrder, recentQuery]);
  const visibleIds = new Set(filteredMedia.map((m) => m.messageId));
  const grouped = useMemo(() => {
    const out: { key: string; items: Media[] }[] = [];
    for (const m of filteredMedia) {
      const key = m.groupedId || m.messageId;
      const prev = out.at(-1);
      if (prev?.key === key) prev.items.push(m);
      else out.push({ key, items: [m] });
    }
    return out;
  }, [filteredMedia]);

  const installEngine = async () => {
    setLoading("正在从官方发布页安装 tdl…");
    setError(undefined);
    try {
      const v = await rpc<Engine>("engine.install", { version: "v0.20.4" });
      setEngine(v);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading("");
    }
  };
  const chooseEngine = async () => {
    const path = await open({
      multiple: false,
      filters: [{ name: "tdl", extensions: ["exe"] }],
    });
    if (typeof path === "string") {
      await rpc("engine.use", { version: path });
      await refreshBootstrap();
    }
  };
  const refreshChats = async () => {
    if (!active || refreshingChats) return;
    setRefreshingChats(true);
    await loadChats(active.id, ++chatGeneration.current, true);
  };
  const savePreference = (key: string, value: string) => void rpc("config.set", {key, value}).catch((e) => setError(String(e)));
  const chooseFolder = (id: string) => {setFolderId(id);if (active) savePreference(`ui.folder.${active.id}`, id);};
  const deleteLogin = async () => {
    if (!removeAccount || removingAccount) return;
    setRemovingAccount(true);
    try {
      removedAccountIds.current.add(removeAccount.id);
      await rpc("accounts.remove", {id: removeAccount.id});
      if (active?.id === removeAccount.id) {
        browserRequests.current.invalidate(); browserScope.current = ""; chatGeneration.current++;
        setChat(undefined); setMedia([]); setChats([]); setFolders([]); setTopicId(""); setOperation(undefined);setPlan(undefined);setShowPlan(false);setMediaPreview(undefined);
      }
      setLogin(emptyLogin); setRemoveAccount(undefined);
      await refreshBootstrap();
    } catch (e) {setError(String(e));} finally {setRemovingAccount(false);}
  };
  const temporaryRule = (): Rule => ({
    ...rule,
    id: "",
    name: "",
    accountId: active!.id,
    chatId: chat!.id,
    topicId,
    order: rule.order || "oldest",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    rootDir: rule.rootDir || boot!.settings.downloadRoot,
    template: rule.template || DEFAULT_TEMPLATE,
    from: dateISO(rule.from),
    to: dateISO(rule.to, true),
  });
  const startOperation = async (
    method: string,
    params: unknown,
    c = chat!,
    t = topicId,
    autoDownload = false,
  ) => {
    if (operationStarting.current || operation) return;
    operationStarting.current = true;
    setActionBusy(true);
    const key = `${c.accountId}/${c.id}/${t}`;
    try {
      const result = await rpc<{ operationId: string }>(method, params);
      if (browserScope.current !== key) {
        await rpc("media.operation.cancel", { id: result.operationId });
        return;
      }
      setOperation({
        id: result.operationId,
        scope: key,
        chat: c,
        topic: t,
        autoDownload,
      });
    } catch (e) {
      setError(String(e));
    } finally {
      operationStarting.current = false;
      setActionBusy(false);
    }
  };
  useEffect(() => {
    if (!operation) return;
    if (operation.scope !== scope) {
      void rpc("media.operation.cancel", { id: operation.id }).catch(() => {});
      setOperation(undefined);
      return;
    }
    let done = false,
      checking = false;
    const finish = (result: MediaOperation) => {
      if (
        done ||
        result.operationId !== operation.id ||
        result.state === "running"
      )
        return;
      done = true;
      setOperation(undefined);
      if (browserScope.current !== operation.scope) return;
      if (result.state === "completed") {
        if (result.plan) {
          setPlan(result.plan);
          if (operation.autoDownload && result.plan.selectedFiles > 0)
            void startPlanDownload(result.plan);
          else {
            setShowPlan(true);
            if (operation.autoDownload)
              setError("没有需要下载的文件，请检查清单中的排除原因");
          }
        }
        void loadMedia(operation.chat, 0, operation.topic);
      } else if (result.state === "failed") {
        setScanError(result.error || "扫描失败"); setError(result.error || "扫描失败");
      }
    };
    const check = async () => {
      if (checking || done) return;
      checking = true;
      try {
        finish(
          await rpc<MediaOperation>("media.operation.get", {
            id: operation.id,
          }),
        );
      } catch (e) {
        if (!done) {
          setError(String(e));
          setOperation(undefined);
        }
      } finally {
        checking = false;
      }
    };
    const off = onWorkerEvent((e) => {
      if (e.operation) finish(e.operation);
    });
    void check();
    const timer = setInterval(check, 1000);
    return () => {
      done = true;
      off();
      clearInterval(timer);
    };
  }, [operation, scope]);
  const scanMedia = async (rescan = false) => {
    if (!active || !chat) return;
    setScanError("");
    await startOperation("media.scan.start", {
      accountId: active.id,
      chatId: chat.id,
      topicId,
      from: dateISO(rule.from),
      to: dateISO(rule.to, true),
      lastN: rule.lastN || 0,
      rescan,
    });
  };
  const changeTopic = async (t: string) => {
    if (!chat) return;
    setOnlySelected(false);
    void loadMedia(chat, 0, t);
    if (t)
      await startOperation(
        "media.scan.start",
        {
          accountId: chat.accountId,
          chatId: chat.id,
          topicId: t,
          rescan: true,
        },
        chat,
        t,
      );
  };
  const previewSelected = async () => {
    if (
      !active ||
      !chat ||
      !boot ||
      !selection.selected.size ||
      actionLock.current
    )
      return;
    actionLock.current = true;
    setActionBusy(true);
    const key = browserScope.current;
    try {
      const p = await rpc<DownloadPlan>("media.previewSelection", {
        rule: temporaryRule(),
        messageIds: [...selection.selected.keys()],
      });
      if (browserScope.current === key) {
        setPlan(p);
        setShowPlan(true);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      actionLock.current = false;
      setActionBusy(false);
    }
  };
  const previewAfter = (m: Media) => {
    selection.clear();setSelectionMode(false);setOnlySelected(false);
    const next: Rule = {
      ...temporaryRule(),
      from: m.date,
      to: new Date().toISOString(),
      recentDays: 0,
      lastN: 0,
      minMessageId: 0,
      maxMessageId: 0,
    };
    setRule((previous) => ({
      ...previous,
      from: next.from,
      to: next.to,
      recentDays: 0,
      lastN: 0,
      minMessageId: 0,
      maxMessageId: 0,
    }));
    setContextMenu(undefined);
    setTimeConfirm({ media: m, rule: next });
  };
  const preview = async () => {
    if (!active || !chat || !boot || actionLock.current) return;
    actionLock.current = true;
    setActionBusy(true);
    const previewScope = browserScope.current;
    const sameRule = rule.accountId === active.id && rule.chatId === chat.id;
    const full: Rule = {
      id: sameRule && rule.id ? rule.id : makeID("rule"),
      name: rule.name || `${chat.visibleName} 下载`,
      accountId: active.id,
      chatId: chat.id,
      topicId,
      order: rule.order || "oldest",
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      rootDir: rule.rootDir || boot.settings.downloadRoot,
      template: rule.template || DEFAULT_TEMPLATE,
      from: dateISO(rule.from),
      to: dateISO(rule.to, true),
      recentDays: rule.recentDays || 0,
      lastN: rule.lastN || 0,
      minMessageId: rule.minMessageId || 0,
      maxMessageId: rule.maxMessageId || 0,
      kinds: rule.kinds || [],
      includeExt: rule.includeExt || [],
      excludeExt: rule.excludeExt || [],
      includeKeyword: rule.includeKeyword || "",
      excludeKeyword: rule.excludeKeyword || "",
      minFileSize: rule.minFileSize || 0,
      maxFileSize: rule.maxFileSize || 0,
      maxFiles: rule.maxFiles || 0,
      maxTotalSize: rule.maxTotalSize || 0,
    };
    setLoading("正在生成固定下载清单…");
    try {
      await rpc("rules.save", full);
      setRule(full);
      setBoot((v) =>
        v
          ? {
              ...v,
              rules: [full, ...v.rules.filter((saved) => saved.id !== full.id)],
            }
          : v,
      );
      const p = await rpc<DownloadPlan>("media.preview", { ruleId: full.id });
      if (browserScope.current === previewScope) {
        setPlan(p);
        setShowPlan(true);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading("");
      actionLock.current = false;
      setActionBusy(false);
    }
  };
  const togglePlan = (messageId: string) =>
    setPlan((p) => {
      if (!p) return p;
      const items = p.items.map((x) =>
        x.media.messageId === messageId &&
        ["selected", "excluded"].includes(x.status)
          ? {
              ...x,
              selected: !x.selected,
              status: x.selected ? "excluded" : "selected",
              reason: x.selected ? "手动排除" : undefined,
            }
          : x,
      );
      return {
        ...p,
        items,
        selectedFiles: items.filter((x) => x.selected).length,
        selectedBytes: items
          .filter((x) => x.selected)
          .reduce((n, x) => n + x.media.size, 0),
      };
    });
  const startPlanDownload = async (plan: DownloadPlan) => {
    if (actionLock.current) return;
    actionLock.current = true;
    setActionBusy(true);
    setLoading("正在创建下载任务…");
    try {
      await rpc("plans.select", {
        id: plan.id,
        messageIds: plan.items
          .filter((x) => x.selected)
          .map((x) => x.media.messageId),
      });
      const job = await rpc<Job>("jobs.create", { planId: plan.id });
      setJobs((v) => [job, ...v]);
      setShowPlan(false);
      setShowJobs(true);
      await rpc("jobs.start", { id: job.id });
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading("");
      actionLock.current = false;
      setActionBusy(false);
    }
  };
  const switchAccount = async (id: string) => {
    try {
      await rpc("accounts.use", { id });
      activeAccountId.current = id;
      browserRequests.current.invalidate();
      browserScope.current = "";
      setChats([]);
      setActive(accounts.find((x) => x.id === id));
      setChat(undefined);
      setTopicId("");
      setMedia([]);
      setOnlySelected(false);
      setShowPlan(false);
    } catch (e) {
      setError(String(e));
    }
  };
  const beginLogin = async () => {
    setLogin((v) => ({
      ...v,
      busy: true,
      error: undefined,
      qrCode: undefined,
      inputKind: undefined,
      prompt: "正在连接 Telegram…",
    }));
    try {
      let accountId = login.accountId;
      if (!accountId) {
        const a = await rpc<Account>("accounts.add", {
          name: login.name || "Telegram 账户",
          namespace: "",
        });
        accountId = a.id;
        setAccounts((v) => [...v, a]);
        setLogin((v) => ({ ...v, accountId }));
      }
      const result = await rpc<{ loginId: string }>("accounts.login.start", {
        accountId,
        method: login.method,
        phone: login.phone,
        desktopPath: login.desktopPath,
        desktopPasscode: login.desktopPasscode,
        proxy: login.proxy ?? boot?.settings.proxy ?? "",
      });
      setLogin((v) =>
        v.open && !v.error ? { ...v, accountId, loginId: result.loginId } : v,
      );
    } catch (e) {
      setLogin((v) => ({
        ...v,
        busy: false,
        loginId: undefined,
        error: String(e),
      }));
    }
  };
  const submitLogin = async (value: string, kind = login.inputKind) => {
    if (!login.loginId || !kind) return;
    setLogin((v) => ({ ...v, busy: true }));
    try {
      await rpc("accounts.login.submit", {
        loginId: login.loginId,
        kind,
        value,
      });
    } catch (e) {
      setLogin((v) => ({ ...v, busy: false, error: String(e) }));
    }
  };
  const runningJobs = jobs.filter((j) => j.state === "running");
  const exitSaving = shutdownApp;

  return (
    <div className="app-shell">
      <FolderRail folders={folders} selected={folderId} onSelect={chooseFolder} />
      <aside className="sidebar">
        <div className="account-bar">
          <button className="icon-button">
            <Menu size={22} />
          </button>
          <select
            value={active?.id || ""}
            onChange={(e) => switchAccount(e.target.value)}
            aria-label="当前账户"
          >
            <option value="">选择账户</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.displayName}
              </option>
            ))}
          </select>
          <button
            className="icon-button"
            title="添加账户"
            onClick={() => setLogin({ ...emptyLogin, open: true })}
          >
            <Plus size={20} />
          </button>
        </div>
        <div className="search">
          <Search size={17} />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索聊天"
          />
          <button onClick={refreshChats} disabled={refreshingChats} title="刷新聊天">
            <RefreshCw size={16} className={refreshingChats ? "spin" : ""} />
          </button>
        </div>
        <div className="chat-sort"><select aria-label="聊天排序" value={chatOrder} onChange={(e) => {setChatOrder(e.target.value);savePreference("ui.chat.order",e.target.value);}}><option value="recent">最近消息优先</option><option value="name">名称 A–Z</option></select></div>
        {chatSyncError && <div className="sync-error" role="status">{chatSyncError}</div>}
        <div
          ref={chatListRef}
          className="chat-list"
          onScroll={(e) =>
            setChatWindowStart(
              Math.max(0, Math.floor(e.currentTarget.scrollTop / 66) - 3),
            )
          }
        >
          {filteredChats.map((c) => (
            <button
              key={c.id}
              className={`chat-row ${chat?.id === c.id ? "active" : ""}`}
              onClick={() => {
                setOnlySelected(false);
                setShowPlan(false);
                void loadMedia(c, 0, "");
              }}
            >
              <span className={`avatar ${c.type}`}>
                {avatars[c.id] ? (
                  <img src={imageSource(avatars[c.id])} alt="" />
                ) : (
                  c.visibleName.slice(0, 1).toUpperCase()
                )}
              </span>
              <span>
                <span className="chat-title"><strong>{c.visibleName}</strong>{c.lastMessageAt && !c.lastMessageAt.startsWith("0001-") && <time title={new Date(c.lastMessageAt).toLocaleString()}>{new Date(c.lastMessageAt).toLocaleDateString() === new Date().toLocaleDateString() ? new Date(c.lastMessageAt).toLocaleTimeString([], {hour:"2-digit",minute:"2-digit"}) : new Date(c.lastMessageAt).toLocaleDateString([], {month:"2-digit",day:"2-digit"})}</time>}</span>
                <small>{c.username ? `@${c.username}` : c.type}</small>
              </span>
            </button>
          ))}
          {active && !!chats.length && !filteredChats.length && <div className="empty-small">{query ? "没有匹配的聊天" : "此分组没有聊天"}</div>}
          {active && !chats.length && (
            <div className="empty-small">刷新以载入聊天列表</div>
          )}
        </div>
        <div className="sidebar-footer">
          <button onClick={() => setShowJobs(true)}>
            <Download size={18} />
            下载任务{runningJobs.length > 0 && <b>{runningJobs.length}</b>}
            <small className="total-speed" title={speedHint}>{speedLabel(runningJobs.reduce((n,j) => n+(j.speedBytesPerSecond || 0),0))}</small>
          </button>
          <button onClick={() => setShowSettings(true)}>
            <Settings size={18} />
            设置
          </button>
        </div>
      </aside>

      <main className="conversation">
        <header className="conversation-header">
          {chat ? (
            <>
              <div>
                <h2>{chat.visibleName}</h2>
                <span>当前已加载 {filteredMedia.length} 条媒体消息</span>
              </div>
              <div className="header-actions">
                <select aria-label="消息顺序" value={mediaOrder} onChange={(e) => {setMediaOrder(e.target.value);savePreference("ui.media.order",e.target.value);}}><option value="newest">倒序（从新到旧）</option><option value="oldest">正序（从旧到新）</option></select>
                <select
                  aria-label="下载状态"
                  value={downloadedFilter}
                  onChange={(e) => setDownloadedFilter(e.target.value)}
                >
                  <option value="all">全部</option>
                  <option value="done">已下载</option>
                  <option value="pending">未下载</option>
                </select>
                {chat.topics && chat.topics.length > 0 && (
                  <select
                    value={topicId}
                    disabled={!!operation || actionBusy}
                    onChange={(e) => changeTopic(e.target.value)}
                  >
                    <option value="">全部话题</option>
                    {chat.topics.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.title}
                      </option>
                    ))}
                  </select>
                )}
                <button
                  disabled={!!operation || actionBusy}
                  onClick={() => scanMedia(false)}
                >
                  <RefreshCw size={17} />
                  扫描新增
                </button>
                <button
                  disabled={!!operation || actionBusy}
                  onClick={() => scanMedia(true)}
                >
                  <Archive size={17} />
                  重扫历史
                </button>
                <button
                  className="primary"
                  disabled={!!operation || actionBusy}
                  onClick={preview}
                >
                  <Download size={17} />
                  预览全部筛选结果
                </button>
              </div>
            </>
          ) : (
            <div>
              <h2>TDL Media</h2>
              <span>选择一个聊天开始浏览</span>
            </div>
          )}
        </header>
        {chat && selectionMode && (
          <div className="selection-toolbar">
            <div>
              <strong>已选 {selection.selected.size} 项</strong>
              <span>
                {bytes(
                  [...selection.selected.values()].reduce(
                    (sum, m) => sum + m.size,
                    0,
                  ),
                )}
              </span>
              <span>
                含隐藏{" "}
                {
                  [...selection.selected.keys()].filter(
                    (id) => !visibleIds.has(id),
                  ).length
                }{" "}
                项
              </span>
            </div>
            <div>
              <button onClick={() => selection.selectAll(filteredMedia)}>
                全选已加载结果
              </button>
              <button onClick={() => selection.invert(filteredMedia)}>
                反选
              </button>
              <button onClick={selection.clear}>清空</button>
              <button
                onClick={() => {
                  selection.clear();
                  setOnlySelected(false);
                  setSelectionMode(false);
                }}
              >
                退出多选
              </button>
              <label>
                <input
                  type="checkbox"
                  checked={onlySelected}
                  onChange={(e) => setOnlySelected(e.target.checked)}
                />
                只看已选
              </label>

              <button
                className="primary"
                disabled={!selection.selected.size || actionBusy || !!operation}
                onClick={previewSelected}
              >
                下载所选
              </button>
            </div>
          </div>
        )}
        {operation && (
          <div className="operation-banner">
            <LoaderCircle size={16} className="spin" />
            正在扫描媒体并准备清单…
            <button
              onClick={() =>
                rpc("media.operation.cancel", { id: operation.id }).catch((e) =>
                  setError(String(e)),
                )
              }
            >
              取消扫描
            </button>
          </div>
        )}
        <section className="message-stream" ref={streamRef}>
          {!chat ? (
            <Welcome
              engine={engine}
              onInstall={installEngine}
              onChoose={chooseEngine}
              onLogin={() => setLogin({ ...emptyLogin, open: true })}
              hasAccount={!!active}
            />
          ) : (
            <>
              {grouped.map((group) => (
                <MediaCard
                  key={group.key}
                  items={group.items}
                  selected={selection.selected}
                  onToggle={(m, shift) =>
                    selection.toggle(m, filteredMedia, shift)
                  }
                  onGroup={() => selection.toggleGroup(group.items)}
                  onOpen={(m) => {
                    if (m.localPath)
                      void openPath(m.localPath).catch((e) =>
                        setError(String(e)),
                      );
                    else if (m.thumbPath) setMediaPreview(m);
                  }}
                  selectionMode={selectionMode}
                  onContext={(media, x, y) =>
                    setContextMenu({
                      media,
                      x: Math.min(x, window.innerWidth - 230),
                      y: Math.min(y, window.innerHeight - 105),
                    })
                  }
                  imageSource={imageSource}
                  icon={kindIcon}
                  bytes={bytes}
                />
              ))}
              {!grouped.length && (
                <div className="empty-state">
                  <Archive size={54} />
                  <h3>
                    {operation ? "正在扫描媒体…" : scanError ? "扫描失败" : (media.length || indexedMediaCount) ? "没有符合条件的媒体" : "尚无可显示的媒体"}
                  </h3>
                  <p>
                    {operation ? "扫描完成后显示媒体。" : scanError ? `${scanError}，可点击“扫描新增”重试。` : (media.length || indexedMediaCount) ? "调整筛选条件以显示更多媒体。" : "点击‘扫描新增’读取聊天中的媒体。"}
                    {topicId && "旧索引缺少话题归属时，点击‘重扫历史’。"}
                  </p>
                </div>
              )}
              {hasMoreMedia && (
                <button
                  className="load-more"
                  disabled={!!loading}
                  onClick={() => loadMedia(chat, mediaOffset, topicId)}
                >
                  <ChevronDown />
                  {mediaOrder === "oldest" ? "加载更新的媒体" : "加载更早的媒体"}
                </button>
              )}
            </>
          )}
        </section>
      </main>

      <aside className="filters-panel">
        <div className="panel-title">
          <SlidersHorizontal size={19} />
          <strong>筛选与保存</strong>
        </div>
        <label>
          保存的规则
          <select
            value={rule.id || ""}
            onChange={(e) => {
              const saved = boot?.rules.find((r) => r.id === e.target.value);
              if (saved) {
                setRule(saved);
                const target = chats.find((c) => c.id === saved.chatId);
                if (target) void loadMedia(target, 0, saved.topicId || "");
              } else setRule((v) => ({ ...v, id: undefined }));
            }}
          >
            <option value="">选择已有规则…</option>
            {boot?.rules
              .filter((r) => !active || r.accountId === active.id)
              .map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name}
                </option>
              ))}
          </select>
        </label>
        <label>
          规则名称
          <input
            value={rule.name || ""}
            onChange={(e) => setRule((v) => ({ ...v, name: e.target.value }))}
            placeholder={chat ? `${chat.visibleName} 下载` : "下载规则"}
          />
        </label>
        <label>
          开始时间
          <input
            type="datetime-local"
            step="1"
            value={dateInput(rule.from)}
            onChange={(e) => setRule((v) => ({ ...v, from: e.target.value }))}
          />
        </label>
        <label>
          结束时间
          <input
            type="datetime-local"
            step="1"
            value={dateInput(rule.to)}
            onChange={(e) => setRule((v) => ({ ...v, to: e.target.value }))}
          />
        </label>
        <label>
          最近天数
          <input
            type="number"
            min="0"
            value={rule.recentDays || ""}
            onChange={(e) =>
              setRule((v) => ({ ...v, recentDays: Number(e.target.value) }))
            }
            placeholder="不限制"
          />
        </label>
        <label>
          最近媒体数
          <input
            type="number"
            min="0"
            value={rule.lastN || ""}
            onChange={(e) =>
              setRule((v) => ({ ...v, lastN: Number(e.target.value) }))
            }
            placeholder="不限制"
          />
        </label>
        <div className="two-cols">
          <label>
            消息 ID 从
            <input
              type="number"
              min="0"
              value={rule.minMessageId || ""}
              onChange={(e) =>
                setRule((v) => ({ ...v, minMessageId: Number(e.target.value) }))
              }
            />
          </label>
          <label>
            消息 ID 到
            <input
              type="number"
              min="0"
              value={rule.maxMessageId || ""}
              onChange={(e) =>
                setRule((v) => ({ ...v, maxMessageId: Number(e.target.value) }))
              }
            />
          </label>
        </div>
        <fieldset>
          <legend>媒体类型</legend>
          <div className="kind-grid">
            {kinds.map(([id, name]) => (
              <label
                key={id}
                className={rule.kinds?.includes(id) ? "selected" : ""}
              >
                <input
                  type="checkbox"
                  checked={rule.kinds?.includes(id) || false}
                  onChange={() =>
                    setRule((v) => ({
                      ...v,
                      kinds: v.kinds?.includes(id)
                        ? v.kinds.filter((x) => x !== id)
                        : [...(v.kinds || []), id],
                    }))
                  }
                />
                {kindIcon(id, 16)}
                {name}
              </label>
            ))}
          </div>
        </fieldset>
        <label>
          包含关键词
          <input
            value={rule.includeKeyword || ""}
            onChange={(e) =>
              setRule((v) => ({ ...v, includeKeyword: e.target.value }))
            }
            placeholder="文件名或消息文字"
          />
        </label>
        <label>
          排除关键词
          <input
            value={rule.excludeKeyword || ""}
            onChange={(e) =>
              setRule((v) => ({ ...v, excludeKeyword: e.target.value }))
            }
          />
        </label>
        <label>
          只包含扩展名
          <input
            value={(rule.includeExt || []).join(",")}
            onChange={(e) =>
              setRule((v) => ({
                ...v,
                includeExt: e.target.value
                  .split(",")
                  .map((x) => x.trim())
                  .filter(Boolean),
              }))
            }
            placeholder="jpg,png,mp4"
          />
        </label>
        <label>
          排除扩展名
          <input
            value={(rule.excludeExt || []).join(",")}
            onChange={(e) =>
              setRule((v) => ({
                ...v,
                excludeExt: e.target.value
                  .split(",")
                  .map((x) => x.trim())
                  .filter(Boolean),
              }))
            }
            placeholder="zip,exe"
          />
        </label>
        <div className="two-cols">
          <label>
            最小 MiB
            <input
              type="number"
              min="0"
              value={rule.minFileSize ? rule.minFileSize / 1048576 : ""}
              onChange={(e) =>
                setRule((v) => ({
                  ...v,
                  minFileSize: Number(e.target.value) * 1048576,
                }))
              }
            />
          </label>
          <label>
            最大 MiB
            <input
              type="number"
              min="0"
              value={rule.maxFileSize ? rule.maxFileSize / 1048576 : ""}
              onChange={(e) =>
                setRule((v) => ({
                  ...v,
                  maxFileSize: Number(e.target.value) * 1048576,
                }))
              }
            />
          </label>
        </div>
        <div className="two-cols">
          <label>
            最多文件
            <input
              type="number"
              min="0"
              value={rule.maxFiles || ""}
              onChange={(e) =>
                setRule((v) => ({ ...v, maxFiles: Number(e.target.value) }))
              }
            />
          </label>
          <label>
            总量 GiB
            <input
              type="number"
              min="0"
              value={rule.maxTotalSize ? rule.maxTotalSize / 1073741824 : ""}
              onChange={(e) =>
                setRule((v) => ({
                  ...v,
                  maxTotalSize: Number(e.target.value) * 1073741824,
                }))
              }
            />
          </label>
        </div>
        <label>
          下载顺序
          <select
            value={rule.order}
            onChange={(e) => setRule((v) => ({ ...v, order: e.target.value }))}
          >
            <option value="oldest">从旧到新</option>
            <option value="newest">从新到旧</option>
          </select>
        </label>
        <label>
          下载根目录
          <span className="path-input">
            <input
              value={rule.rootDir || boot?.settings.downloadRoot || ""}
              onChange={(e) =>
                setRule((v) => ({ ...v, rootDir: e.target.value }))
              }
            />
            <button
              onClick={async () => {
                const p = await open({ directory: true });
                if (typeof p === "string")
                  setRule((v) => ({ ...v, rootDir: p }));
              }}
            >
              <FolderOpen size={17} />
            </button>
          </span>
        </label>
        <label>
          命名模板
          <textarea
            rows={3}
            value={rule.template}
            onChange={(e) =>
              setRule((v) => ({ ...v, template: e.target.value }))
            }
          />
        </label>
        <button
          className="primary full"
          disabled={!chat || actionBusy || !!operation}
          onClick={preview}
        >
          <Download size={18} />
          生成下载清单
        </button>
      </aside>

      {contextMenu && (
        <div
          className="message-context-menu"
          role="menu"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          onClick={(e) => e.stopPropagation()}
        >
          <button
            role="menuitem"
            autoFocus
            onClick={() => {
              setSelectionMode(true);
              selection.selectAll([contextMenu.media]);
              setContextMenu(undefined);
            }}
          >
            <Check size={18} />
            选择消息
          </button>
          <button
            role="menuitem"
            disabled={actionBusy || !!operation}
            onClick={() => previewAfter(contextMenu.media)}
          >
            <Download size={18} />
            从此条开始下载
          </button>
        </div>
      )}
      {timeConfirm && (
        <div className="modal-backdrop">
          <div
            className="confirm-card"
            role="dialog"
            aria-labelledby="time-confirm-title"
          >
            <h3 id="time-confirm-title">从此时间开始下载？</h3>
            <p>起止时间已填入右侧筛选。</p>
            <p>
              {new Date(timeConfirm.rule.from!).toLocaleString()} 至{" "}
              {new Date(timeConfirm.rule.to!).toLocaleString()}
            </p>
            <p>
              包含起始消息及同一秒的消息。将按当前类型、关键词、大小和总量限制补扫，完成后开始下载。
            </p>
            <div>
              <button onClick={() => setTimeConfirm(undefined)}>取消</button>
              <button
                className="primary"
                disabled={actionBusy}
                onClick={() => {
                  const pending = timeConfirm;
                  setTimeConfirm(undefined);
                  void startOperation(
                    "media.previewAfter",
                    {
                      rule: pending.rule,
                      anchorMessageId: pending.media.messageId,
                      to: pending.rule.to,
                    },
                    chat!,
                    topicId,
                    true,
                  );
                }}
              >
                开始下载
              </button>
            </div>
          </div>
        </div>
      )}
      {loading && (
        <div className="loading-toast">
          <LoaderCircle className="spin" size={18} />
          {loading}
        </div>
      )}
      {error && (
        <div className="error-toast">
          <CircleAlert size={18} />
          <span>{error}</span>
          <button onClick={() => setError(undefined)}>
            <X size={17} />
          </button>
        </div>
      )}
      {login.open && (
        <LoginModal
          state={{ ...login, proxy: login.proxy ?? boot?.settings.proxy ?? "" }}
          accounts={accounts}
          setState={setLogin}
          onStart={beginLogin}
          onRemove={(id) => setRemoveAccount(accounts.find((a) => a.id === id))}
          onSubmit={submitLogin}
          onClose={() => {
            if (login.loginId)
              rpc("accounts.login.cancel", { loginId: login.loginId });
            setLogin(emptyLogin);
          }}
        />
      )}
      {removeAccount && <div className="modal-backdrop"><div className="confirm-card" role="dialog" aria-labelledby="remove-title"><h3 id="remove-title">删除登录信息？</h3><p>将从账户列表移除“{removeAccount.displayName}”，并清除本应用保存的登录会话。索引、规则、任务和下载文件保留，重新登录同一 Telegram 账户后可继续使用。</p><p>该账户正在进行的下载会暂停。</p><div><button disabled={removingAccount} onClick={() => setRemoveAccount(undefined)}>取消</button><button className="danger" disabled={removingAccount} onClick={deleteLogin}>{removingAccount ? "正在删除…" : "删除登录信息"}</button></div></div></div>}
      {mediaPreview?.thumbPath && (
        <div
          className="modal-backdrop image-preview"
          onClick={() => setMediaPreview(undefined)}
        >
          <button className="modal-close">
            <X />
          </button>
          <img
            src={imageSource(mediaPreview.thumbPath)}
            alt={mediaPreview.fileName}
          />
          <p>{mediaPreview.fileName}</p>
        </div>
      )}
      {showPlan && plan && (
        <PlanDrawer
          busy={actionBusy}
          plan={plan}
          onToggle={togglePlan}
          onClose={() => setShowPlan(false)}
          onStart={() => {
            if (plan) void startPlanDownload(plan);
          }}
        />
      )}
      {showJobs && (
        <JobsDrawer
          jobs={jobs}
          onClose={() => setShowJobs(false)}
          onError={setError}
        />
      )}
      {showSettings && boot && (
        <SettingsModal
          version={boot.version || "0.3.2"}
          updateResult={boot.updateResult}
          onUpdate={shutdownApp}
          settings={boot.settings}
          engine={engine}
          onClose={() => setShowSettings(false)}
          onInstall={installEngine}
          onChoose={chooseEngine}
          onClear={async () => {
            await rpc("cache.clear");
            setMedia((v) => v.map((m) => ({ ...m, thumbPath: "" })));
          }}
        />
      )}
      {closeChoice && (
        <div className="modal-backdrop">
          <div className="confirm-card">
            <h3>下载仍在进行</h3>
            <p>你可以让任务留在托盘继续运行，或者保存当前进度后退出。</p>
            <div>
              <button onClick={exitSaving}>保存进度并退出</button>
              <button
                className="primary"
                onClick={async () => {
                  setCloseChoice(false);
                  await invoke("hide_main");
                }}
              >
                后台继续下载
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Welcome({
  engine,
  onInstall,
  onChoose,
  onLogin,
  hasAccount,
}: {
  engine?: Engine;
  onInstall: () => void;
  onChoose: () => void;
  onLogin: () => void;
  hasAccount: boolean;
}) {
  return (
    <div className="welcome">
      <div className="paper-plane">➤</div>
      <h1>Telegram 媒体，清楚地下载</h1>
      <p>
        浏览聊天中的媒体，按时间、类型和大小筛选，再用官方 tdl 引擎可靠下载。
      </p>
      {!engine ? (
        <div className="welcome-actions">
          <button className="primary" onClick={onInstall}>
            <Download size={18} />
            安装 tdl v0.20.4
          </button>
          <button onClick={onChoose}>
            <FolderOpen size={18} />
            使用已有 tdl.exe
          </button>
        </div>
      ) : !hasAccount ? (
        <button className="primary" onClick={onLogin}>
          <LogIn size={18} />
          登录 Telegram
        </button>
      ) : (
        <p className="ready">
          <Check size={18} />
          已就绪，请在左侧刷新并选择聊天
        </p>
      )}
    </div>
  );
}

function LoginModal({
  state,
  accounts,
  setState,
  onStart,
  onSubmit,
  onClose,
  onRemove,
}: {
  state: LoginState;
  accounts: Account[];
  setState: React.Dispatch<React.SetStateAction<LoginState>>;
  onStart: () => void;
  onSubmit: (v: string, k?: string) => void;
  onClose: () => void;
  onRemove: (id: string) => void;
}) {
  const [value, setValue] = useState("");
  return (
    <div className="modal-backdrop">
      <div className="modal login-modal">
        <button className="modal-close" onClick={onClose}>
          <X />
        </button>
        <h2>登录 Telegram</h2>
        <p>凭据只保存在本机，不会进入日志和下载报告。</p>
        {state.accountId && <button className="delete-login" disabled={state.busy} onClick={() => onRemove(state.accountId!)}>删除登录信息</button>}
        {!state.loginId ? (
          <>
            <label>
              账户配置
              <select
                value={state.accountId || "new"}
                onChange={(e) =>
                  setState((v) => ({
                    ...v,
                    accountId:
                      e.target.value === "new" ? undefined : e.target.value,
                  }))
                }
              >
                <option value="new">添加新账户</option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.displayName}
                  </option>
                ))}
              </select>
            </label>
            {!state.accountId && (
              <label>
                显示名称
                <input
                  value={state.name}
                  onChange={(e) =>
                    setState((v) => ({ ...v, name: e.target.value }))
                  }
                  placeholder="我的 Telegram"
                />
              </label>
            )}
            <div className="login-tabs">
              {[
                ["qr", "扫码"],
                ["code", "验证码"],
                ["desktop", "桌面导入"],
              ].map(([id, name]) => (
                <button
                  key={id}
                  className={state.method === id ? "active" : ""}
                  onClick={() =>
                    setState((v) => ({
                      ...v,
                      method: id as LoginState["method"],
                    }))
                  }
                >
                  {name}
                </button>
              ))}
            </div>
            {state.method === "code" && (
              <label>
                手机号
                <input
                  value={state.phone}
                  onChange={(e) =>
                    setState((v) => ({ ...v, phone: e.target.value }))
                  }
                  placeholder="+86 138…"
                />
              </label>
            )}
            {state.method === "desktop" && (
              <>
                <label>
                  Telegram Desktop 目录
                  <input
                    value={state.desktopPath}
                    onChange={(e) =>
                      setState((v) => ({ ...v, desktopPath: e.target.value }))
                    }
                    placeholder="留空自动查找"
                  />
                </label>
                <label>
                  Desktop 本地密码
                  <input
                    type="password"
                    value={state.desktopPasscode}
                    onChange={(e) =>
                      setState((v) => ({
                        ...v,
                        desktopPasscode: e.target.value,
                      }))
                    }
                    placeholder="未设置则留空"
                  />
                </label>
              </>
            )}
            <label>
              代理地址（留空直连）
              <input
                value={state.proxy || ""}
                onChange={(e) =>
                  setState((v) => ({ ...v, proxy: e.target.value }))
                }
                placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
              />
              <small>
                默认读取已启用的系统代理。TUN 无法连接时，可填写代理软件的
                HTTP/SOCKS5 地址；此设置也用于浏览和下载。
              </small>
            </label>
            <button
              className="primary full"
              disabled={
                state.busy ||
                (!state.accountId && !state.name) ||
                (state.method === "code" && !state.phone)
              }
              onClick={onStart}
            >
              {state.busy ? <LoaderCircle className="spin" /> : <LogIn />}
              开始登录
            </button>
          </>
        ) : (
          <div className="login-step">
            {state.qrCode && <img className="qr" src={state.qrCode} />}
            <h3>{state.prompt}</h3>
            {state.choices?.map((c) => (
              <button
                className="choice"
                key={c}
                onClick={() => onSubmit(c, "desktopAccount")}
              >
                {c}
              </button>
            ))}
            {state.inputKind && state.inputKind !== "desktopAccount" && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  onSubmit(value);
                  setValue("");
                }}
              >
                <input
                  autoFocus
                  type={state.inputKind === "password" ? "password" : "text"}
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder={
                    state.inputKind === "code" ? "验证码" : "两步验证密码"
                  }
                />
                <button className="primary" disabled={!value || state.busy}>
                  提交
                </button>
              </form>
            )}
          </div>
        )}
        {state.error && <p className="inline-error">{state.error}</p>}
      </div>
    </div>
  );
}

function PlanDrawer({
  plan,
  onToggle,
  onClose,
  onStart,
  busy,
}: {
  busy: boolean;
  plan: DownloadPlan;
  onToggle: (id: string) => void;
  onClose: () => void;
  onStart: () => void;
}) {
  return (
    <div className="drawer-backdrop">
      <aside className="drawer plan-drawer">
        <header>
          <div>
            <h2>下载清单</h2>
            {plan.from && !plan.from.startsWith("0001") && (
              <p>
                {new Date(plan.from).toLocaleString()} 至{" "}
                {new Date(plan.to!).toLocaleString()}（含起始时间）
              </p>
            )}
            <p>
              {plan.selectedFiles} 个文件 · {bytes(plan.selectedBytes)}，
              {plan.existingFiles} 个已存在
            </p>
          </div>
          <button onClick={onClose}>
            <X />
          </button>
        </header>
        <div className="plan-list">
          {plan.items.map((x) => (
            <label
              key={x.media.messageId}
              className={`plan-row ${x.selected ? "selected" : ""}`}
            >
              <input
                type="checkbox"
                disabled={!["selected", "excluded"].includes(x.status)}
                checked={x.selected}
                onChange={() => onToggle(x.media.messageId)}
              />
              <span className="plan-kind">{kindIcon(x.media.kind)}</span>
              <span>
                <strong>{x.media.fileName}</strong>
                <small>
                  {new Date(x.media.date).toLocaleString()} ·{" "}
                  {bytes(x.media.size)}
                </small>
                <small className="target">{x.targetPath || x.reason}</small>
              </span>
              <em>{x.selected ? "下载" : x.reason || x.status}</em>
            </label>
          ))}
        </div>
        <footer>
          <span>固定清单创建后，恢复任务仍使用这份清单。</span>
          <button
            className="primary"
            disabled={!plan.selectedFiles || busy}
            onClick={onStart}
          >
            <Download />
            开始下载
          </button>
        </footer>
      </aside>
    </div>
  );
}

function SettingsModal({
  settings,
  engine,
  onClose,
  onInstall,
  onChoose,
  onClear,
  version,
  updateResult,
  onUpdate,
}: {
  version: string;
  updateResult: Bootstrap["updateResult"];
  onUpdate: () => Promise<void>;
  settings: Bootstrap["settings"];
  engine?: Engine;
  onClose: () => void;
  onInstall: () => void;
  onChoose: () => void;
  onClear: () => void;
}) {
  return (
    <div className="modal-backdrop">
      <div className="modal settings-modal">
        <button className="modal-close" onClick={onClose}>
          <X />
        </button>
        <h2>设置与引擎</h2>
        <UpdatePanel
          version={version}
          result={updateResult}
          onApply={onUpdate}
        />
        <dl>
          <dt>数据目录</dt>
          <dd>{settings.dataDir}</dd>
          <dt>默认下载目录</dt>
          <dd>{settings.downloadRoot}</dd>
          <dt>缓存上限</dt>
          <dd>{bytes(settings.cacheMaxBytes)}</dd>
          <dt>重试次数</dt>
          <dd>{settings.retries}</dd>
          <dt>tdl 引擎</dt>
          <dd>{engine ? `${engine.version} · ${engine.path}` : "尚未安装"}</dd>
        </dl>
        <div className="modal-actions">
          <button onClick={onClear}>
            <X />
            清理预览缓存
          </button>
          <button onClick={onChoose}>
            <FolderOpen />
            指定 tdl.exe
          </button>
          <button className="primary" onClick={onInstall}>
            <RefreshCw />
            安装/更新 v0.20.4
          </button>
        </div>
      </div>
    </div>
  );
}
