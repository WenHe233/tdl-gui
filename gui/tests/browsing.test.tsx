import { expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { browseChats, withSystemFolders, speedLabel } from "../src/browsing";
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

it("separates archived chats while preserving custom folder membership and pins", () => {
 const data: Chat[] = [chats[0], {...chats[1], archived:true}, {...chats[2], archived:true, pinnedOrder:1},
  {accountId:"a",id:"4",type:"group",visibleName:"Gamma",archived:true,pinnedOrder:2}];
 const ids=(folder:string,order="recent",query="")=>browseChats(data,folders,folder,order,query).map(c=>c.id);
 expect(ids("all")).toEqual(["1"]);
 expect(ids("archived")).toEqual(["3","4","2"]);
 expect(ids("archived","name")).toEqual(["3","2","4"]);
 expect(ids("archived","recent","beta")).toEqual(["2"]);
 expect(ids("all","recent","beta")).toEqual([]);
 expect(ids("f")).toEqual(["3","2"]);
 expect(ids("","name")).toEqual(["3","2","4","1"]);
 expect(browseChats(data.slice(1),[],"all","recent","")).toEqual([]);
 expect(browseChats(chats,[],"archived","recent","")).toEqual([]);
});

it("always places the archive after all without reordering custom folders or duplicating entries", () => {
 const all:ChatFolder={id:"all",title:"全部聊天",chatIds:[],pinnedIds:[]};
 const archive:ChatFolder={id:"archived",title:"已归档",chatIds:["2"],pinnedIds:[]};
 expect(withSystemFolders([]).map(f=>f.id)).toEqual(["all","archived"]);
 const input=[archive,folders[0],all,{...folders[0],id:"last"}];
 expect(withSystemFolders(input).map(f=>f.id)).toEqual(["f","all","archived","last"]);
 expect(input[0]).toBe(archive);
 const select=vi.fn();render(<FolderRail folders={[]} selected="archived" onSelect={select}/>);
 const button=screen.getByRole("button",{name:"已归档"});
 expect(button.getAttribute("aria-pressed")).toBe("true");
 fireEvent.click(button);expect(select).toHaveBeenCalledWith("archived");
});
