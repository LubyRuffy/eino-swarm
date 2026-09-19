const MATH_LANGS = new Set(["math", "latex", "tex", "katex"])

export function isMathFence(lang: string): boolean {
  return MATH_LANGS.has(lang.trim().toLowerCase())
}
