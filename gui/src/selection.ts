import { useState } from "react";
import type { Media } from "./types";

export function useSelection(scope: string) {
  const [state, setState] = useState<{
    scope: string;
    items: Map<string, Media>;
    anchor?: string;
  }>({ scope, items: new Map() });
  if (state.scope !== scope) setState({ scope, items: new Map() });
  const current =
    state.scope === scope
      ? state
      : { scope, items: new Map<string, Media>(), anchor: undefined };
  const change = (
    fn: (items: Map<string, Media>) => void,
    anchor = current.anchor,
  ) =>
    setState((previous) => {
      const items = new Map(previous.scope === scope ? previous.items : []);
      fn(items);
      return { scope, items, anchor };
    });
  const toggle = (media: Media, visible: Media[], shift = false) => {
    const a = visible.findIndex((m) => m.messageId === current.anchor),
      b = visible.findIndex((m) => m.messageId === media.messageId);
    const range =
      shift && a >= 0 && b >= 0
        ? visible.slice(Math.min(a, b), Math.max(a, b) + 1)
        : [media];
    const checked = !current.items.has(media.messageId);
    change(
      (items) =>
        range.forEach((m) =>
          checked ? items.set(m.messageId, m) : items.delete(m.messageId),
        ),
      media.messageId,
    );
  };
  return {
    selected: current.items,
    toggle,
    clear: () => setState({ scope, items: new Map() }),
    selectAll: (media: Media[]) =>
      change((items) => media.forEach((m) => items.set(m.messageId, m))),
    invert: (media: Media[]) =>
      change((items) =>
        media.forEach((m) =>
          items.has(m.messageId)
            ? items.delete(m.messageId)
            : items.set(m.messageId, m),
        ),
      ),
    toggleGroup: (media: Media[]) => {
      const all = media.every((m) => current.items.has(m.messageId));
      change((items) =>
        media.forEach((m) =>
          all ? items.delete(m.messageId) : items.set(m.messageId, m),
        ),
      );
    },
  };
}

export class RequestScope {
  private generation = 0;
  private key = "";
  enter(key: string, refresh = false) {
    if (key !== this.key || refresh) {
      this.key = key;
      this.generation++;
    }
    return this.generation;
  }
  valid(token: number) {
    return token === this.generation;
  }
  invalidate() {
    this.generation++;
  }
}
