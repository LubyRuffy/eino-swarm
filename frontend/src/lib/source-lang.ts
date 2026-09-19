/** File suffix → highlighter dialect. Unknown suffixes stay plain text;
 *  guessing a language from the body is how a log becomes a Christmas tree. */

export type LangId =
  | "c"
  | "css"
  | "docker"
  | "go"
  | "gomod"
  | "html"
  | "java"
  | "js"
  | "json"
  | "php"
  | "python"
  | "ruby"
  | "rust"
  | "shell"
  | "sql"
  | "toml"
  | "yaml"

export interface LangDef {
  keywords: Set<string>
  lineComment?: string
  blockComment?: [string, string]
  backticks?: "raw" | "template"
  tripleQuotes?: boolean
  hashbang?: boolean
  caseInsensitive?: boolean
  dollarIdent?: boolean
  hexColors?: boolean
  stringPrefixes?: boolean
  /** After these words, the next identifier is a name (`func Hello`). */
  nameLeaders: Set<string>
  /** Identifier followed by this op is a key (`name:` in yaml). */
  keyOp?: ":" | "="
  /** A leading # plus identifier is a preprocessor line (`#include`). */
  hashDirective?: boolean
}

const NAMED: Record<string, LangId> = {
  dockerfile: "docker",
  makefile: "shell",
  gnumakefile: "shell",
  "go.mod": "gomod",
  "go.sum": "gomod",
  ".gitignore": "yaml",
  ".dockerignore": "yaml",
  ".env": "yaml",
}

const EXT: Record<string, LangId> = {
  bash: "shell",
  c: "c",
  cc: "c",
  cpp: "c",
  cs: "java",
  css: "css",
  cxx: "c",
  fish: "shell",
  go: "go",
  h: "c",
  hpp: "c",
  htm: "html",
  html: "html",
  ini: "toml",
  java: "java",
  js: "js",
  json: "json",
  jsonc: "json",
  jsx: "js",
  ksh: "shell",
  kt: "java",
  kts: "java",
  less: "css",
  mjs: "js",
  mts: "js",
  php: "php",
  py: "python",
  pyi: "python",
  rb: "ruby",
  rs: "rust",
  scss: "css",
  sh: "shell",
  sql: "sql",
  svg: "html",
  swift: "java",
  toml: "toml",
  ts: "js",
  tsx: "js",
  vue: "html",
  xml: "html",
  yaml: "yaml",
  yml: "yaml",
  zsh: "shell",
}

function words(s: string): Set<string> {
  return new Set(s.split(/\s+/).filter(Boolean))
}

const LANGS: Record<Exclude<LangId, "shell" | "html">, LangDef> = {
  c: {
    keywords: words(
      "auto break case char const continue default do double else enum extern float for goto if inline int long register return short signed sizeof static struct switch typedef union unsigned void volatile while bool true false NULL nullptr class namespace public private protected virtual template typename this new delete try catch throw const_cast static_cast dynamic_cast reinterpret_cast using",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    hashDirective: true,
    nameLeaders: words("class struct enum namespace"),
  },
  css: {
    keywords: words(
      "important inherit initial unset revert auto none block flex grid absolute relative fixed sticky from to var calc rgb rgba hsl hsla url min max clamp",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    hexColors: true,
    nameLeaders: new Set(),
  },
  docker: {
    keywords: words(
      "FROM AS RUN CMD ENTRYPOINT COPY ADD ENV ARG WORKDIR USER VOLUME EXPOSE LABEL ONBUILD STOPSIGNAL HEALTHCHECK SHELL MAINTAINER",
    ),
    lineComment: "#",
    caseInsensitive: true,
    nameLeaders: words("from as"),
  },
  go: {
    keywords: words(
      "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr true false nil iota append cap clear close complex copy delete imag len make max min new panic print println real recover",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    backticks: "raw",
    nameLeaders: words("func type interface struct package"),
  },
  gomod: {
    keywords: words("module go require replace exclude retract toolchain"),
    lineComment: "//",
    nameLeaders: words("module"),
  },
  java: {
    keywords: words(
      "abstract assert boolean break byte case catch char class const continue default do double else enum extends final finally float for goto if implements import instanceof int interface long native new package private protected public return short static strictfp super switch synchronized this throw throws transient try void volatile while true false null var let override fun data object companion sealed inner operator infix suspend lateinit where by as in is out typealias",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    nameLeaders: words("class interface enum fun func function struct"),
  },
  js: {
    keywords: words(
      "break case catch class const continue debugger default delete do else export extends false finally for function if import in instanceof let new null return static super switch this throw true try typeof undefined var void while with yield async await of from as enum interface type implements private protected public readonly abstract namespace declare module any never unknown keyof infer satisfies",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    backticks: "template",
    hashbang: true,
    nameLeaders: words("class function async"),
  },
  json: {
    keywords: words("true false null"),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    nameLeaders: new Set(),
  },
  php: {
    keywords: words(
      "abstract and array as break case catch class clone const continue declare default die do echo else elseif empty enddeclare endfor endforeach endif endswitch endwhile eval exit extends final finally fn for foreach function global goto if implements include include_once instanceof insteadof interface isset list match namespace new or print private protected public readonly require require_once return static switch throw trait try unset use var while xor yield true false null php",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    dollarIdent: true,
    hashbang: true,
    nameLeaders: words("function class fn"),
  },
  python: {
    keywords: words(
      "False None True and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield match case",
    ),
    lineComment: "#",
    tripleQuotes: true,
    hashbang: true,
    stringPrefixes: true,
    nameLeaders: words("def class async"),
  },
  ruby: {
    keywords: words(
      "BEGIN END alias and begin break case class def defined do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield",
    ),
    lineComment: "#",
    hashbang: true,
    dollarIdent: true,
    nameLeaders: words("def class module"),
  },
  rust: {
    keywords: words(
      "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while union box yield try",
    ),
    lineComment: "//",
    blockComment: ["/*", "*/"],
    nameLeaders: words("fn struct enum trait type impl mod"),
  },
  sql: {
    keywords: words(
      "select from where and or not null as in is like between join inner left right full outer on group by order asc desc insert into values update set delete create table index view drop alter add constraint primary key foreign references unique default limit offset union all distinct having case when then else end exists join",
    ),
    lineComment: "--",
    blockComment: ["/*", "*/"],
    caseInsensitive: true,
    nameLeaders: new Set(),
  },
  toml: {
    keywords: words("true false inf nan"),
    lineComment: "#",
    nameLeaders: new Set(),
    keyOp: "=",
  },
  yaml: {
    keywords: words("true false null yes no on off"),
    lineComment: "#",
    nameLeaders: new Set(),
    keyOp: ":",
  },
}

export function languageFromPath(path: string): LangId | undefined {
  const base = path.replace(/\\/g, "/").split("/").pop() ?? ""
  const lower = base.toLowerCase()
  if (NAMED[lower]) return NAMED[lower]
  const dot = lower.lastIndexOf(".")
  if (dot <= 0) return undefined
  return EXT[lower.slice(dot + 1)]
}

/** Fence info-string → highlighter dialect. Unknown tags stay plain text;
 *  the body is not inspected, same rule as a file suffix. */
const FENCE_ALIAS: Record<string, LangId> = {
  "c#": "java",
  "c++": "c",
  console: "shell",
  csharp: "java",
  golang: "go",
  javascript: "js",
  kotlin: "java",
  objc: "c",
  "objective-c": "c",
  python: "python",
  python3: "python",
  ruby: "ruby",
  rust: "rust",
  shell: "shell",
  typescript: "js",
}

export function languageFromFence(tag: string): LangId | undefined {
  const key = fenceTag(tag)
  if (!key) return undefined
  const aliased = FENCE_ALIAS[key]
  if (aliased) return aliased
  if (EXT[key]) return EXT[key]
  if (key === "shell" || key === "html") return key
  if (Object.hasOwn(LANGS, key)) return key as LangId
  return undefined
}

function fenceTag(tag: string): string {
  const raw = tag.trim().toLowerCase()
  if (!raw) return ""
  const token = raw.split(/[^a-z0-9+#._-]+/)[0] ?? ""
  return token.replace(/^\./, "")
}

export function langDef(id: LangId): LangDef | undefined {
  if (id === "shell" || id === "html") return undefined
  return LANGS[id]
}
