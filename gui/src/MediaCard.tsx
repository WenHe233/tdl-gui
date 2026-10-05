import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import type { Media } from "./types";

export function MediaCard({
  items,
  selected,
  onToggle,
  onGroup,
  onOpen,
  onContext,
  selectionMode,
  imageSource,
  icon,
  bytes,
}: {
  items: Media[];
  selected: Map<string, Media>;
  onToggle: (m: Media, shift: boolean) => void;
  onGroup: () => void;
  onOpen: (m: Media) => void;
  onContext: (m: Media, x: number, y: number) => void;
  selectionMode: boolean;
  imageSource: (s: string) => string;
  icon: (s: string, n: number) => ReactNode;
  bytes: (n: number) => string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  const count = items.filter((m) => selected.has(m.messageId)).length;
  useEffect(() => {
    if (ref.current)
      ref.current.indeterminate = count > 0 && count < items.length;
  }, [count, items.length]);
  return (
    <article className="message-card">
      {selectionMode && items.length > 1 && (
        <label className="album-select">
          <input
            ref={ref}
            type="checkbox"
            aria-label="选择整组"
            checked={count === items.length}
            onChange={onGroup}
          />
          选择相册{" "}
          <span>
            {count}/{items.length}
          </span>
        </label>
      )}
      <div className={`media-grid count-${Math.min(items.length, 4)}`}>
        {items.map((m) => (
          <div
            key={m.messageId}
            className={`media-entry ${!m.thumbPath && ["document","audio","voice"].includes(m.kind)?"no-preview":""} ${selectionMode && selected.has(m.messageId) ? "is-selected" : ""}`}
            onContextMenu={(e) => {
              e.preventDefault();
              onContext(m, e.clientX, e.clientY);
            }}
          >
            <button
              className="media-tile"
              aria-label={`预览 ${m.fileName}`}
              onClick={(e) =>
                selectionMode ? onToggle(m, e.shiftKey) : onOpen(m)
              }
            >
              {m.thumbPath ? (
                <img src={imageSource(m.thumbPath)} alt={m.fileName} />
              ) : (
                <span>{icon(m.kind, 38)}</span>
              )}
              <i>{bytes(m.size)}</i>
              {m.downloaded && <em title="已下载">✓</em>}
            </button>
            {selectionMode && (
              <input
                className="media-select"
                type="checkbox"
                aria-label={`选择 ${m.fileName}`}
                checked={selected.has(m.messageId)}
                onClick={(e) => onToggle(m, e.shiftKey)}
                onChange={() => {}}
              />
            )}
            <div className="media-actions">
              <span title={m.fileName}>{m.fileName}</span>
            </div>
          </div>
        ))}
      </div>
      <div className="message-info">
        {items[0].caption && <p>{items[0].caption}</p>}
        <small>
          {new Date(items[0].date).toLocaleString()} · {items.length} 个媒体
        </small>
      </div>
    </article>
  );
}
