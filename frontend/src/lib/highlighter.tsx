import * as React from "react"
import {
  createHighlighterCore,
  type GrammarState,
  type HighlighterCore,
} from "shiki/core"
import { createJavaScriptRegexEngine } from "shiki/engine/javascript"

/** Kept as one string so each app's Tailwind scan can emit both theme colors. */
export const shikiTokenClass = "text-(--shiki-light) dark:text-(--shiki-dark)"

// Each language loads the first time a fence asks for it. Names match Shiki's
// grammar ids. A tag that is not listed stays plain text.
const languages = {
  asm: () => import("shiki/langs/asm.mjs"),
  bash: () => import("shiki/langs/shellscript.mjs"),
  c: () => import("shiki/langs/c.mjs"),
  cpp: () => import("shiki/langs/cpp.mjs"),
  csharp: () => import("shiki/langs/csharp.mjs"),
  css: () => import("shiki/langs/css.mjs"),
  diff: () => import("shiki/langs/diff.mjs"),
  dockerfile: () => import("shiki/langs/dockerfile.mjs"),
  go: () => import("shiki/langs/go.mjs"),
  html: () => import("shiki/langs/html.mjs"),
  java: () => import("shiki/langs/java.mjs"),
  javascript: () => import("shiki/langs/javascript.mjs"),
  json: () => import("shiki/langs/json.mjs"),
  jsx: () => import("shiki/langs/jsx.mjs"),
  kotlin: () => import("shiki/langs/kotlin.mjs"),
  markdown: () => import("shiki/langs/markdown.mjs"),
  php: () => import("shiki/langs/php.mjs"),
  python: () => import("shiki/langs/python.mjs"),
  ruby: () => import("shiki/langs/ruby.mjs"),
  rust: () => import("shiki/langs/rust.mjs"),
  sql: () => import("shiki/langs/sql.mjs"),
  toml: () => import("shiki/langs/toml.mjs"),
  tsx: () => import("shiki/langs/tsx.mjs"),
  typescript: () => import("shiki/langs/typescript.mjs"),
  xml: () => import("shiki/langs/xml.mjs"),
  yaml: () => import("shiki/langs/yaml.mjs"),
}

type LangName = keyof typeof languages

const aliases: Record<string, LangName> = {
  assembly: "asm",
  "c#": "csharp",
  "c++": "cpp",
  cc: "cpp",
  console: "bash",
  cs: "csharp",
  cxx: "cpp",
  docker: "dockerfile",
  gas: "asm",
  golang: "go",
  h: "c",
  hpp: "cpp",
  js: "javascript",
  kt: "kotlin",
  kts: "kotlin",
  md: "markdown",
  nasm: "asm",
  py: "python",
  python3: "python",
  rb: "ruby",
  rs: "rust",
  sh: "bash",
  shell: "bash",
  shellscript: "bash",
  ts: "typescript",
  x86asm: "asm",
  yasm: "asm",
  yml: "yaml",
  zsh: "bash",
}

let highlighter: Promise<HighlighterCore> | undefined

function getHighlighter() {
  highlighter ??= createHighlighterCore({
    themes: [
      import("shiki/themes/github-light.mjs"),
      import("shiki/themes/github-dark.mjs"),
    ],
    langs: [],
    engine: createJavaScriptRegexEngine({ forgiving: true }),
  })
  return highlighter
}

function getLanguage(language: string): LangName | "ansi" | null {
  const name = language.trim().toLowerCase()
  if (!name || name === "text" || name === "plain" || name === "plaintext") return null
  if (name === "ansi") return name
  if (name in languages) return name as LangName
  return aliases[name] ?? null
}

// Loads a language and returns a highlighter for it, or null for a language
// that is not listed.
export async function loadHighlighter(language: string) {
  const lang = getLanguage(language)
  if (!lang) return null

  const shiki = await getHighlighter()
  const shikiLang = lang === "bash" ? "shellscript" : lang
  if (lang !== "ansi" && !shiki.getLoadedLanguages().includes(shikiLang)) {
    await shiki.loadLanguage(languages[lang])
    // The first tokenize after a grammar loads paints the line in one color.
    // Discard that pass so the fence the person sees uses the real scopes.
    shiki.codeToTokens(" ", {
      lang: shikiLang,
      themes: { light: "github-light", dark: "github-dark" },
      defaultColor: false,
    })
  }

  return {
    // One React node per line, plus the grammar state after the last line.
    // Pass that state back to continue where streamed code left off.
    highlight(code: string, state?: GrammarState) {
      const tokenize = (text: string) =>
        shiki.codeToTokens(text, {
          lang: shikiLang,
          themes: { light: "github-light", dark: "github-dark" },
          defaultColor: false,
          grammarState: state,
        }).tokens

      // ANSI has no grammar state, so each line is tokenized on its own and
      // gets the same colors whether it arrives in one piece or streams.
      const tokens =
        lang === "ansi"
          ? code.split("\n").flatMap((line) => tokenize(line))
          : tokenize(code)

      return {
        lines: tokens.map((line) =>
          line.map((token, index) => (
            <span
              key={index}
              style={token.htmlStyle as React.CSSProperties}
              className={shikiTokenClass}
            >
              {token.content}
            </span>
          )),
        ),
        state: shiki.getLastGrammarState(tokens),
      }
    },
  }
}

export async function highlightCode(code: string, language: string) {
  const loaded = await loadHighlighter(language)
  return loaded ? loaded.highlight(code).lines : null
}
