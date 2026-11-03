import { buildHtmlArtifact, HTML_ARTIFACT_SANDBOX, PACKAGE_DOCUMENT_SANDBOX } from "./html-artifact"

const origin = "http://127.0.0.1:8787"

describe("html artifact", () => {
  it("keeps inline content and a strict sandbox without the app origin", async () => {
    const srcdoc = await buildHtmlArtifact(
      '<div id="preview-script-target">document</div><script>parent.__previewExecuted = true</script><base href="http://127.0.0.1:8787/"><meta http-equiv="refresh" content="0;url=/">',
      "page.html",
      origin,
      async () => null,
      new AbortController().signal,
    )
    expect(HTML_ARTIFACT_SANDBOX).not.toMatch(/allow-same-origin|allow-top-navigation|allow-popups/)
    expect(srcdoc).toContain("preview-script-target")
    expect(srcdoc).toContain("Content-Security-Policy")
    expect(srcdoc).toContain("connect-src 'none'")
    expect(srcdoc).not.toContain("<base")
    expect(srcdoc).not.toContain("http-equiv=\"refresh\"")
    expect(srcdoc).toContain("parent.__previewExecuted")
  })

  it("inlines a local stylesheet and drops an app-origin script", async () => {
    const requested: string[] = []
    const srcdoc = await buildHtmlArtifact(
      '<link rel="stylesheet" href="theme.css"><link rel="preload" href="/assets/app.js" as="script"><script src="http://127.0.0.1:8787/api/threads"></script><a href="javascript:alert(1)">x</a><img src="pic.png">',
      "docs/page.html",
      origin,
      async (path) => {
        requested.push(path)
        if (path === "docs/theme.css") return new TextEncoder().encode("p { color: rgb(1, 2, 3); background: url(bg.png); }")
        if (path === "docs/bg.png") return Uint8Array.from([1, 2, 3])
        return null
      },
      new AbortController().signal,
    )
    expect(requested).toContain("docs/theme.css")
    expect(requested).toContain("docs/bg.png")
    expect(requested).not.toContain("../secret")
    expect(srcdoc).toContain("rgb(1, 2, 3)")
    expect(srcdoc).toContain("data:image/png;base64,")
    expect(srcdoc).not.toContain("127.0.0.1:8787")
    expect(srcdoc).not.toContain("javascript:")
    expect(srcdoc).not.toContain("/assets/app.js")
    expect(srcdoc).not.toContain("pic.png")
  })

  it("removes package scripts for a static document", async () => {
    const srcdoc = await buildHtmlArtifact(
      '<p>Chapter</p><script>bad()</script><img src="cover.png">',
      "OEBPS/c1.xhtml",
      origin,
      async (path) => path === "OEBPS/cover.png" ? Uint8Array.from([9]) : null,
      new AbortController().signal,
      { scripts: false },
    )
    expect(PACKAGE_DOCUMENT_SANDBOX).toBe("")
    expect(srcdoc).toContain("Chapter")
    expect(srcdoc).not.toContain("<script")
    expect(srcdoc).toContain("script-src 'none'")
    expect(srcdoc).toContain("data:image/png;base64,")
  })

  it("does not request a path outside the package", async () => {
    const requested: string[] = []
    await buildHtmlArtifact('<img src="../../secret.png">', "docs/page.html", origin, async (path) => {
      requested.push(path)
      return null
    }, new AbortController().signal)
    expect(requested).toEqual([])
  })
})
