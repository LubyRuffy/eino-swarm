import { describe, expect, it } from "vitest"

import { langDef, languageFromFence, languageFromPath } from "./source-lang"

describe("source-lang", () => {
  it("maps the suffixes the files tree already treats as code or config", () => {
    expect(languageFromPath("a.c")).toBe("c")
    expect(languageFromPath("a.cpp")).toBe("c")
    expect(languageFromPath("a.java")).toBe("java")
    expect(languageFromPath("a.rs")).toBe("rust")
    expect(languageFromPath("a.rb")).toBe("ruby")
    expect(languageFromPath("a.php")).toBe("php")
    expect(languageFromPath("a.sql")).toBe("sql")
    expect(languageFromPath("a.css")).toBe("css")
    expect(languageFromPath("a.html")).toBe("html")
    expect(languageFromPath("a.json")).toBe("json")
    expect(languageFromPath("a.toml")).toBe("toml")
    expect(languageFromPath("a.sh")).toBe("shell")
    expect(languageFromPath("Makefile")).toBe("shell")
    expect(languageFromPath(".env")).toBe("yaml")
  })

  it("does not hand a dialect to the markup/shell special cases", () => {
    expect(langDef("html")).toBeUndefined()
    expect(langDef("shell")).toBeUndefined()
    expect(langDef("go")?.lineComment).toBe("//")
  })

  it("maps a fence tag onto the same dialect as a suffix", () => {
    expect(languageFromFence("cpp")).toBe("c")
    expect(languageFromFence("javascript")).toBe("js")
    expect(languageFromFence("chart")).toBeUndefined()
  })
})
