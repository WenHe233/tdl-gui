import type { Chat, ChatFolder } from "./types";

export function withSystemFolders(folders: ChatFolder[]): ChatFolder[] {
  const all = folders.find((f) => f.id === "all") || { id: "all", title: "全部聊天", chatIds: [], pinnedIds: [] };
  const archived = folders.find((f) => f.id === "archived") || { id: "archived", title: "已归档", chatIds: [], pinnedIds: [] };
  const list = folders.filter((f) => f.id !== "archived");
  if (!list.some((f) => f.id === "all")) list.unshift(all);
  list.splice(list.findIndex((f) => f.id === "all") + 1, 0, archived);
  return list;
}

export function browseChats(chats: Chat[], folders: ChatFolder[], folder: string, order: string, query: string) {
  const selected = folders.find((f) => f.id === folder);
  const members = new Set(selected?.chatIds || []);
  const pins = new Map((selected?.pinnedIds || []).map((id, i) => [id, i + 1]));
  const q = query.trim().toLocaleLowerCase();
  const systemFolder = !folder || folder === "all" || folder === "archived";
  const includes = (c: Chat) => !folder || (folder === "all" ? !c.archived : folder === "archived" ? !!c.archived : members.has(c.id));
  return chats.filter((c) => c.visibleName.trim() && includes(c) &&
    (!q || c.visibleName.toLocaleLowerCase().includes(q) || (c.username || "").toLocaleLowerCase().includes(q)))
    .sort((a, b) => {
      if (order === "recent") {
        const pa = systemFolder ? a.pinnedOrder || 0 : pins.get(a.id) || 0;
        const pb = systemFolder ? b.pinnedOrder || 0 : pins.get(b.id) || 0;
        if (pa !== pb && (pa || pb)) return pa ? pb ? pa - pb : -1 : 1;
        const stamp = (v?: string) => v && !v.startsWith("0001-") ? Date.parse(v) || 0 : 0;
        const delta = stamp(b.lastMessageAt) - stamp(a.lastMessageAt);
        if (delta) return delta;
      }
      return a.visibleName.localeCompare(b.visibleName, "zh-CN", { numeric: true, sensitivity: "base" }) || a.id.localeCompare(b.id);
    });
}

export function formatBytes(n: number) {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.min(units.length - 1, Math.max(0, Math.floor(Math.log(n) / Math.log(1024))));
  return `${(n / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}`;
}
export const speedLabel = (n?: number) => `${formatBytes(n || 0)}/s`;
export const speedHint = "按最近 2 秒的文件写入增量估算";
