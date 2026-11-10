import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { ProjectManager } from "./project-manager"
import type { RemoteResponse } from "@/lib/rpc"
import type { RemoteLink } from "@/lib/link"

import { setLocale } from "@/lib/i18n"
beforeEach(() => setLocale("zh"))

function fixture() {
 const rpc = vi.fn(async (req: { op: string }): Promise<RemoteResponse> => ({v:1,id:"r",ok:true,
  ...(req.op === "catalog" ? { memory_available:true, models:[{provider_id:"a",model:"first",default:true},{provider_id:"b",model:"second"}] } : {}),
  ...(req.op === "project_get" ? {memory_available:true,project:{id:"p",name:"Existing",system_prompt:"Keep",workdir:"/chosen ",resolved_workdir:"/chosen ",memory_enabled:true,nuwa_enabled:false}} : {}),
  ...(req.op === "project_create" ? {projects:[{id:"new",name:"New"}]} : {}),
 }))
 const link = {alive:()=>true,rpc} as unknown as RemoteLink
 return {link,rpc}
}
describe("phone project management",()=>{
 it("creates on the selected PC with distinct Nuwa models and keeps path whitespace",async()=>{
  const {link,rpc}=fixture(); const onSaved=vi.fn()
  render(<ProjectManager link={link} projects={[]} onSaved={onSaved} onClose={()=>{}} />)
  fireEvent.click(screen.getByRole("button",{name:"新建项目"}))
  fireEvent.change(await screen.findByLabelText("项目名称"),{target:{value:"New"}})
  fireEvent.change(screen.getByLabelText("电脑工作目录"),{target:{value:"/chosen "}})
  await waitFor(()=>expect(screen.getByLabelText("项目记忆")).toBeChecked())
  fireEvent.click(screen.getByLabelText("女娲协作"))
  fireEvent.click(screen.getByRole("button",{name:"创建项目"}))
  await waitFor(()=>expect(onSaved).toHaveBeenCalled())
  expect(rpc).toHaveBeenCalledWith(expect.objectContaining({op:"project_create",project_patch:expect.objectContaining({name:"New",workdir:"/chosen ",nuwa_enabled:true,nuwa_planner_provider_id:"a",nuwa_executor_provider_id:"b"})}),expect.anything())
 })
 it("name-only edits preserve a concurrently updated instruction",async()=>{
  const {link,rpc}=fixture()
  render(<ProjectManager link={link} projects={[{id:"p",name:"Existing"}]} onSaved={()=>{}} onClose={()=>{}} />)
  fireEvent.click(screen.getByRole("button",{name:"Existing"}))
  await screen.findByDisplayValue("Keep")
  fireEvent.change(screen.getByLabelText("项目名称"),{target:{value:"Renamed"}})
  fireEvent.click(screen.getByRole("button",{name:"保存项目"}))
  await waitFor(()=>expect(rpc).toHaveBeenCalledWith({op:"project_update",project_id:"p",project_patch:{name:"Renamed"}},expect.anything()))
 })
 it("shows an old PC refusal locally and allows closing",async()=>{
  const {link,rpc}=fixture()
  rpc.mockResolvedValue({v:1,id:"r",ok:false,code:"unknown_op"} as never)
  const close=vi.fn()
  render(<ProjectManager link={link} projects={[{id:"p",name:"Existing"}]} onSaved={()=>{}} onClose={close} />)
  fireEvent.click(screen.getByRole("button",{name:"Existing"}))
  expect(await screen.findByRole("alert")).toHaveTextContent("请更新电脑端以管理项目。")
  fireEvent.click(screen.getByRole("button",{name:"关闭"}))
  expect(close).toHaveBeenCalled()
 })
})

it("retains failed saves and allows retry without losing the instruction",async()=>{
 const {link,rpc}=fixture(); let fail=true
 const original=rpc.getMockImplementation()!
 rpc.mockImplementation(async(req)=>req.op==="project_update" && fail ? {v:1,id:"r",ok:false,error:"Save refused"} : original(req))
 render(<ProjectManager link={link} projects={[{id:"p",name:"Existing"}]} onSaved={()=>{}} onClose={()=>{}} />)
 fireEvent.click(screen.getByRole("button",{name:"Existing"}))
 fireEvent.change(await screen.findByLabelText("项目指令"),{target:{value:"Changed instruction"}})
 fireEvent.click(screen.getByRole("button",{name:"保存项目"}))
 expect(await screen.findByRole("alert")).toHaveTextContent("Save refused")
 expect(screen.getByLabelText("项目指令")).toHaveValue("Changed instruction")
 fail=false
 // A successful update acknowledges the actual identity.
 rpc.mockResolvedValueOnce({v:1,id:"r",ok:true,projects:[{id:"p",name:"Existing"}]} as never)
 fireEvent.click(screen.getByRole("button",{name:"保存项目"}))
 expect(await screen.findByRole("status")).toHaveTextContent("项目已保存。")
})

it("ignores an old editor response after returning to the project list",async()=>{
 const {link,rpc}=fixture()
 let resolve!: (value: Awaited<ReturnType<typeof rpc>>) => void
 const original=rpc.getMockImplementation()!
 rpc.mockImplementation((req)=>req.op==="project_get" ? new Promise((done)=>{resolve=done}) : original(req))
 render(<ProjectManager link={link} projects={[{id:"p",name:"Existing"}]} onSaved={()=>{}} onClose={()=>{}} />)
 fireEvent.click(screen.getByRole("button",{name:"Existing"}))
 fireEvent.click(screen.getByRole("button",{name:"返回项目列表"}))
 await act(async()=>resolve(await original({op:"project_get"})))
 expect(screen.getByRole("button",{name:"新建项目"})).toBeVisible()
 expect(screen.queryByLabelText("项目指令")).toBeNull()
})

it("does not publish a late save acknowledgement after the editor closes",async()=>{
 const {link,rpc}=fixture()
 let resolve!: (value: RemoteResponse) => void
 const original=rpc.getMockImplementation()!
 rpc.mockImplementation((req)=>req.op==="project_update" ? new Promise((done)=>{resolve=done}) : original(req))
 const onSaved=vi.fn()
 const view=render(<ProjectManager link={link} projects={[{id:"p",name:"Existing"}]} onSaved={onSaved} onClose={()=>{}} />)
 fireEvent.click(screen.getByRole("button",{name:"Existing"}))
 fireEvent.change(await screen.findByLabelText("项目名称"),{target:{value:"New name"}})
 fireEvent.click(screen.getByRole("button",{name:"保存项目"}))
 expect(screen.getByRole("button",{name:"正在保存…"})).toBeDisabled()
 view.unmount()
 await act(async()=>resolve({v:1,id:"r",ok:true,projects:[{id:"p",name:"New name"}]}))
 expect(onSaved).not.toHaveBeenCalled()
})

it("keeps keyboard focus inside the project dialog after a control disappears",()=>{
 const {link}=fixture()
 render(<><button>Background</button><ProjectManager link={link} projects={[]} onSaved={()=>{}} onClose={()=>{}} /></>)
 screen.getByRole("button",{name:"Background"}).focus()
 fireEvent.keyDown(window,{key:"Tab"})
 expect(screen.getByRole("button",{name:"关闭"})).toHaveFocus()
 fireEvent.keyDown(window,{key:"Tab",shiftKey:true})
 expect(screen.getByRole("button",{name:"新建项目"})).toHaveFocus()
})
