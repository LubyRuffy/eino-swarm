import { writeFileSync } from "node:fs"
import { describe, expect, it } from "vitest"

import { parseSlashSubmit, splitSlash } from "./slash-core"

// Writes seeded random composer texts with this parser's verdict for the Go
// side (internal/slash TestComposerParsesMatchTheEngine) to compare; see
// docs/TESTING.md. The tokens are structure, never a sample request: slash
// variants, whitespace JavaScript and Go classify differently, quotes, code
// fences, paths and URLs.
const TOKENS = [
  "/goal", "/plan", "/compact", "/", "／", "、", "goal", "plan", "GOAL", " ", "\n", "\t",
  "\u3000", "\u00a0", "\ufeff", "\u0085", "\u2028", "a", "中", "x/", "./", '"', "'", "`",
  "```", "~~~", ">", "\\", "(", "]", ":", "https://h/", "/tmp/", "-", "_", "“", "”", "「", "」",
  "😀", "𝐀", "²", "Ⅻ", "\\a", ".",
]

function prng(seed: number) {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const out = process.env.SLASH_DIFF_OUT

describe("composer slash parse corpus", () => {
  it.skipIf(!out)("writes seeded parses for the engine comparison", () => {
    const rand = prng(Number(process.env.SLASH_DIFF_SEED ?? 1))
    const runs = Number(process.env.SLASH_DIFF_RUNS ?? 300000)
    const lines: string[] = []
    for (let n = 0; n < runs; n++) {
      let text = ""
      for (let len = 1 + Math.floor(rand() * 14); len > 0; len--) text += TOKENS[Math.floor(rand() * TOKENS.length)]
      const parsed = parseSlashSubmit(text)
      const leading = splitSlash(text.trimStart())?.name === parsed?.id
      lines.push(JSON.stringify({ text, command: parsed?.id ?? "", arg: parsed?.arg ?? "", leading }))
    }
    writeFileSync(out as string, lines.join("\n") + "\n")
    expect(lines).toHaveLength(runs)
  })
})
