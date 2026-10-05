import { expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { browseChats, speedLabel } from "../src/browsing";
import { FolderRail } from "../src/FolderRail";
import type { Chat, ChatFolder } from "../src/types";

const chats: Chat[] = [
 {accountId:"a",id:"1",type:"group",visibleName:"Zulu",pinnedOrder:1},
 {accountId:"a",id:"2",type:"channel",visibleName:"Beta",lastMessageAt:"2026-10-05T00:00:00Z"},
 {accountId:"a",id:"3",type:"private",visibleName:"Alpha",lastMessageAt:"2026-10-04T00:00:00Z"},
];
const folders: ChatFolder[] = [{id:"f",title:"工作",emoticon:"💼",chatIds:["2","3"],pinnedIds:["3"]}];
it("sorts chats and folder pins without modifying the source", () => {
 expect(browseChats(chats,folders,"all","recent","").map((c)=>c.id)).toEqual(["1","2","3"]);
 expect(browseChats(chats,folders,"all","name","").map((c)=>c.id)).toEqual(["3","2","1"]);
 expect(browseChats(chats,folders,"f","recent","").map((c)=>c.id)).toEqual(["3","2"]);
 expect(browseChats(chats,folders,"f","recent","Zulu")).toHaveLength(0);
 expect(chats.map((c)=>c.id)).toEqual(["1","2","3"]);
});
it("renders folder selection and preserves the provided Telegram order", () => {
 const select=vi.fn();render(<FolderRail folders={[...folders,{id:"all",title:"全部聊天",chatIds:[],pinnedIds:[]}]} selected="f" onSelect={select}/>);
 expect(screen.getAllByRole("button")[0].textContent).toContain("工作");
 expect(screen.getByRole("button",{name:"工作"}).getAttribute("aria-pressed")).toBe("true");
 fireEvent.click(screen.getByRole("button",{name:"全部聊天"}));expect(select).toHaveBeenCalledWith("all");
});
it("formats zero, invalid and fractional download rates", () => {
 expect(speedLabel()).toBe("0 B/s");expect(speedLabel(-1)).toBe("0 B/s");expect(speedLabel(0.1)).toBe("0 B/s");expect(speedLabel(1572864)).toBe("1.5 MiB/s");
});
