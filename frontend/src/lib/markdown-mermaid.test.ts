import { describe, expect, it, vi } from "vitest"

import { mermaidFenceIsClosed, mermaidFitScale, mermaidSourceUsesResources, mermaidSVGDataURL, mermaidZoomBy, readMermaidAppearance, readMermaidFrame } from "./markdown-mermaid"

describe("Mermaid source boundaries", () => {
  it.each([
    'flowchart LR\nA@{ img: "https://example.invalid/pixel" }',
    'flowchart LR\nA@{ "img": "https://example.invalid/pixel" }',
    "flowchart LR\nA@{ 'img': 'https://example.invalid/pixel' }",
    'flowchart LR\nA@{ "i\\u006dg": "https://example.invalid/pixel" }',
    'flowchart LR\nA@{ label: "}", img: "https://example.invalid/pixel" }',
    'flowchart LR\nA["`![Image](https://example.invalid/pixel)`"]',
    'flowchart LR\nA-->B\nclassDef remote fill:url(https://example.invalid/pixel);',
    'flowchart LR\nA-->B\nstyle A fill:url (https://example.invalid/pixel)',
    'flowchart LR\nA-->B\nstyle A fill:u\\72l(https://example.invalid/pixel)',
    'flowchart LR\nA-->B\nlinkStyle 0 fill:url(https://example.invalid/pixel)',
    'C4Context\nPerson(a,"Client")\nUpdateElementStyle(a, $bgColor="url(https://example.invalid/pixel)")',
    'flowchart LR;A-->B;classDef remote fill:u\\72l(https://example.invalid/pixel)',
  ])("retains resource syntax as source: %s", (source) => {
    expect(mermaidSourceUsesResources(source)).toBe(true)
  })

  it.each([
    'graph TB\nsubgraph Layer["服务层"]\nA["Client<br/>请求"] --> B["Worker"]\nend',
    'flowchart LR\nA["https://example.invalid/docs"] --> B["image: diagram"]',
    'flowchart LR\nA["itemurl(group)"] --> B["Shown"]',
    'flowchart LR\nA["itemurl (group)"] --> B["Shown"]',
    'flowchart LR\nA-->B\nclassDef active fill:green,stroke:blue;',
    'flowchart LR\nA@{ shape: rect, label: "image: diagram" } --> B',
    'sequenceDiagram\nClient->>Server: Query',
  ])("allows ordinary labels and local styling: %s", (source) => {
    expect(mermaidSourceUsesResources(source)).toBe(false)
  })

  it.each([
    ["```mermaid\nA-->B\n```", true],
    ["~~~~mermaid\nA-->B\n~~~~~", true],
    ["````mermaid\nA-->B\n```", false],
    ["```mermaid\nA[\"```\"]", false],
    ["```mermaid\nA-->B\n~~~", false],
    ["```mermaid\nA-->B\n``` extra", false],
    ["```mermaid", false],
    ["```mermaid\n> A-->B\n> ```", true],
  ])("checks the actual parser span for closure: %s", (source, closed) => {
    const prefix = "unrelated\n\n"
    expect(mermaidFenceIsClosed(prefix + source + "\nlater", {
      position: { start: { offset: prefix.length }, end: { offset: prefix.length + source.length } },
    })).toBe(closed)
  })

  it("does not assume a missing source span is closed", () => {
    expect(mermaidFenceIsClosed("```mermaid\nA-->B\n```", undefined)).toBe(false)
  })

  it("resolves colours from the current CSS tokens and preserves the chosen font", () => {
    const root = document.documentElement
    root.style.setProperty("--foreground", "120 100% 25%")
    root.style.setProperty("--font-content", "serif")
    root.classList.add("dark")
    try {
      expect(readMermaidAppearance()).toMatchObject({
        dark: true,
        fontFamily: "serif",
        colors: { primaryTextColor: "#008000", textColor: "#008000" },
      })
      expect(document.querySelector("span[hidden]")).toBeNull()
    } finally {
      root.style.removeProperty("--foreground")
      root.style.removeProperty("--font-content")
      root.classList.remove("dark")
    }
  })

  it("gives SVG images their measured intrinsic size and preserves text", () => {
    const url = mermaidSVGDataURL('<svg xmlns="http://www.w3.org/2000/svg" viewBox="-2 3 800.5 400" width="100%" style="max-width:800.5px"><text>甲 &amp; 乙</text></svg>')
    expect(url).toMatch(/^data:image\/svg\+xml;charset=utf-8,/)
    const svg = new DOMParser().parseFromString(decodeURIComponent(url!.split(",")[1]), "image/svg+xml").documentElement
    expect(svg.getAttribute("width")).toBe("800.5")
    expect(svg.getAttribute("height")).toBe("400")
    expect((svg as unknown as SVGElement).style.maxWidth).toBe("")
    expect(svg.textContent).toBe("甲 & 乙")
  })

  it("fits a diagram inside the reading frame without enlarging it", () => {
    expect(mermaidFitScale({ width: 2000, height: 800 }, { width: 500, height: 600 })).toBe(0.25)
    expect(mermaidFitScale({ width: 400, height: 2000 }, { width: 800, height: 500 })).toBe(0.25)
    expect(mermaidFitScale({ width: 200, height: 100 }, { width: 800, height: 500 })).toBe(1)
    expect(mermaidFitScale({ width: 2000, height: 800 }, { width: 0, height: 0 })).toBe(1)
    expect(mermaidFitScale({ width: 0, height: 100 }, { width: 500, height: 500 })).toBe(1)
  })

  it("steps zoom within the manual range", () => {
    expect(mermaidZoomBy(0.4, 1)).toBe(0.65)
    expect(mermaidZoomBy(0.4, -1)).toBe(0.15)
    expect(mermaidZoomBy(3.9, 1)).toBe(4)
    expect(mermaidZoomBy(0.2, -1)).toBe(0.1)
  })

  it("reads the content box under the diagram's max height", () => {
    const element = document.createElement("div")
    element.style.maxHeight = "480px"
    element.style.padding = "12px"
    document.body.appendChild(element)
    const width = vi.spyOn(element, "clientWidth", "get").mockReturnValue(224)
    try {
      expect(readMermaidFrame(element)).toEqual({ width: 200, height: 456 })
    } finally {
      width.mockRestore()
      element.remove()
    }
  })

  it.each([
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 0 20"/>',
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 -2 20"/>',
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 NaN 20"/>',
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20"/>',
    '<svg xmlns="http://www.w3.org/2000/svg"/>',
    '<svg viewBox="0 0 20 20"/>',
    '<div viewBox="0 0 20 20"/>',
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">',
  ])("retains code for malformed image geometry/XML: %s", (svg) => {
    expect(mermaidSVGDataURL(svg)).toBeNull()
  })
})
