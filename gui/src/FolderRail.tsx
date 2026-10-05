import { Archive, Folder, MessagesSquare } from "lucide-react";
import type { ChatFolder } from "./types";
import { withSystemFolders } from "./browsing";

export function FolderRail({ folders, selected, onSelect }: { folders: ChatFolder[]; selected: string; onSelect: (id: string) => void }) {
  const list = withSystemFolders(folders);
  return <nav className="folder-rail" aria-label="聊天分组">{list.map((f) =>
    <button key={f.id} className={selected === f.id ? "active" : ""} aria-label={f.title} aria-pressed={selected === f.id} title={f.title} onClick={() => onSelect(f.id)}>
      {f.id === "all" ? <MessagesSquare size={26} /> : f.id === "archived" ? <Archive size={26} /> : f.emoticon ? <span className="folder-emoji">{f.emoticon}</span> : <Folder size={26} />}
      <span className="folder-name">{f.title}</span>
    </button>)}</nav>;
}
