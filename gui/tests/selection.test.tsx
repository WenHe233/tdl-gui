import { describe, it, expect, vi } from "vitest";
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { useSelection, RequestScope } from "../src/selection";
import { MediaCard } from "../src/MediaCard";
import type { Media } from "../src/types";

const media = Array.from({ length: 125 }, (_, i): Media => ({
  accountId: "a",
  chatId: "c",
  messageId: String(i + 1),
  mediaId: String(i + 1),
  fileName: `${i + 1}.jpg`,
  date: "2026-10-05T00:00:00Z",
  kind: "photo",
  size: 10,
  downloaded: false,
}));
describe("manual selection", () => {
  it("retains selections across pages, filters, and range selection", () => {
    const { result, rerender } = renderHook(
      ({ scope }) => useSelection(scope),
      { initialProps: { scope: "a/c/" } },
    );
    act(() => result.current.toggle(media[0], media.slice(0, 100)));
    act(() => result.current.toggle(media[104], media, true));
    expect(result.current.selected.size).toBe(105);
    act(() => result.current.invert([media[0], media[120]]));
    expect(result.current.selected.has("1")).toBe(false);
    expect(result.current.selected.has("121")).toBe(true);
    rerender({ scope: "a/c/" });
    expect(result.current.selected.size).toBe(105);
    act(() => result.current.selectAll([media[0]]));
    expect(result.current.selected.size).toBe(106);
    act(() => result.current.clear());
    expect(result.current.selected.size).toBe(0);
  });
  it("clears on account/chat/topic changes and never resurrects old selection", () => {
    const { result, rerender } = renderHook(
      ({ scope }) => useSelection(scope),
      { initialProps: { scope: "a/c/" } },
    );
    act(() => result.current.selectAll(media));
    for (const scope of ["a/b/", "a/c/", "b/c/", "a/c/t", "a/c/"]) {
      rerender({ scope });
      expect(result.current.selected.size).toBe(0);
    }
  });
  it("shows mixed album state and handles checkbox shift clicks and anchor action", () => {
    const toggle = vi.fn(),
      group = vi.fn(),
      after = vi.fn(),
      open = vi.fn();
    render(
      <MediaCard
        items={media.slice(0, 3)}
        selected={new Map([["1", media[0]]])}
        onToggle={toggle}
        onGroup={group}
        selectionMode={true}
        onContext={after}
        onOpen={open}

        imageSource={(x) => x}
        icon={() => null}
        bytes={String}
      />,
    );
    const album = screen.getByRole("checkbox", {
      name: "选择整组",
    }) as HTMLInputElement;
    expect(album.indeterminate).toBe(true);
    fireEvent.click(album);
    expect(group).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("checkbox", { name: "选择 3.jpg" }), {
      shiftKey: true,
    });
    expect(toggle).toHaveBeenCalledWith(media[2], true);
    expect(open).not.toHaveBeenCalled();
    fireEvent.contextMenu(screen.getByRole("button", { name: "预览 2.jpg" }), {
      clientX: 100,
      clientY: 120,
    });
    expect(after).toHaveBeenCalledWith(media[1], 100, 120);
  });
  it("rejects late responses from old chats or refresh generations", () => {
    const scope = new RequestScope();
    const a = scope.enter("a/c");
    const page = scope.enter("a/c");
    expect(a).toBe(page);
    scope.enter("a/d");
    expect(scope.valid(a)).toBe(false);
    const b = scope.enter("a/c");
    scope.enter("a/c", true);
    expect(scope.valid(b)).toBe(false);
  });
});
