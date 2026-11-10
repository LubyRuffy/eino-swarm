import { render, screen, waitFor } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { PhoneMarkdown } from "./markdown"
const renderDiagram=vi.hoisted(()=>vi.fn())
vi.mock("../../../frontend/src/lib/markdown-mermaid",async(importOriginal)=>({
 ...await importOriginal<object>(),renderMermaid:renderDiagram,
}))
beforeEach(()=>{
 renderDiagram.mockReset().mockResolvedValue('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 200"><text>Flow</text></svg>')
 vi.stubGlobal("ResizeObserver",class {observe(){} disconnect(){}})
})
it("renders a closed Mermaid fence while retaining its exact source",async()=>{
 const source="flowchart LR\nA[Start] --> B[End]"
 render(<PhoneMarkdown text={'```mermaid\n'+source+'\n```'} />)
 const image=await screen.findByRole("img",{name:"Mermaid"})
 expect(image).toHaveAttribute("src",expect.stringContaining("data:image/svg+xml"))
 expect(screen.getByTestId("phone-mermaid").querySelector("code")?.textContent).toBe(source)
 expect(renderDiagram).toHaveBeenCalledWith(source,expect.anything(),expect.any(Function))
})
it("keeps an incomplete diagram as copyable source without rendering",async()=>{
 render(<PhoneMarkdown text={'```mermaid\nflowchart LR\nA -->'} />)
 expect(screen.getByTestId("markdown-code")).toBeInTheDocument()
 await waitFor(()=>expect(renderDiagram).not.toHaveBeenCalled())
})
it("retains the source when a diagram cannot render",async()=>{
 renderDiagram.mockRejectedValue(new Error("syntax"))
 render(<PhoneMarkdown text={'```mermaid\ninvalid diagram\n```'} />)
 await waitFor(()=>expect(renderDiagram).toHaveBeenCalled())
 expect(screen.getByTestId("markdown-code")).toHaveTextContent("invalid diagram")
 expect(screen.queryByRole("img",{name:"Mermaid"})).toBeNull()
})
