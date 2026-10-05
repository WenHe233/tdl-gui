import type { Chat, ChatFolder } from "./types";

export function browseChats(chats: Chat[], folders: ChatFolder[], folder: string, order: string, query: string) {
  const selected = folders.find((f) => f.id === folder);
  const members = new Set(selected?.chatIds || []);
  const pins = new Map((selected?.pinnedIds || []).map((id, i) => [id, i + 1]));
  const q = query.trim().toLocaleLowerCase();
  return chats.filter((c) => c.visibleName.trim() && (folder === "all" || members.has(c.id)) &&
    (!q || c.visibleName.toLocaleLowerCase().includes(q) || (c.username || "").toLocaleLowerCase().includes(q)))
    .sort((a, b) => {
      if (order === "recent") {
        const pa = folder === "all" ? a.pinnedOrder || 0 : pins.get(a.id) || 0;
        const pb = folder === "all" ? b.pinnedOrder || 0 : pins.get(b.id) || 0;
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
