import { afterEach, describe, expect, it } from "vitest"

import { en } from "./messages-en"
import { zh } from "./messages-zh"
import {
  applyLocale,
  htmlLang,
  localizeNotice,
  normalizeLocalePref,
  readLocalePref,
  resolveLocale,
  t,
  writeLocalePref,
} from "./i18n"
import { toggleLocalePref } from "./use-t"

describe("normalizeLocalePref", () => {
  it("keeps a pinned language and treats anything else as follow-the-system", () => {
    expect(normalizeLocalePref("en")).toBe("en")
    expect(normalizeLocalePref("zh")).toBe("zh")
    expect(normalizeLocalePref("system")).toBe("system")
    expect(normalizeLocalePref("ZH")).toBe("zh")
    expect(normalizeLocalePref("")).toBe("system")
    expect(normalizeLocalePref("fr")).toBe("system")
  })
})

describe("resolveLocale", () => {
  it("follows the browser when the preference is system", () => {
    expect(resolveLocale("en")).toBe("en")
    expect(resolveLocale("zh")).toBe("zh")
    expect(resolveLocale("system", "zh-CN")).toBe("zh")
    expect(resolveLocale("system", "zh")).toBe("zh")
    expect(resolveLocale("system", "en-US")).toBe("en")
    expect(resolveLocale("system", "fr-FR")).toBe("en")
  })
})

describe("t", () => {
  it("returns the English chrome by default and interpolates placeholders", () => {
    expect(t("en", "header.idle")).toBe("Idle")
    expect(t("zh", "header.idle")).toBe("空闲")
    expect(t("en", "queue.count", { n: 3 })).toBe("3 Queued")
    expect(t("zh", "queue.count", { n: 3 })).toBe("3 条排队")
  })

  it("has a Chinese string for every English key", () => {
    const missing = (Object.keys(en) as Array<keyof typeof en>).filter(
      (key) => !zh[key],
    )
    expect(missing).toEqual([])
  })
})

describe("locale preference storage", () => {
  afterEach(() => {
    localStorage.removeItem("zwai.locale")
  })

  it("round-trips a pinned language and ignores junk", () => {
    expect(readLocalePref()).toBe("system")
    writeLocalePref("zh")
    expect(readLocalePref()).toBe("zh")
    localStorage.setItem("zwai.locale", "nope")
    expect(readLocalePref()).toBe("system")
  })
})

describe("applyLocale", () => {
  it("sets html lang so the browser offers the matching spellcheck", () => {
    applyLocale("zh")
    expect(document.documentElement.lang).toBe("zh-CN")
    expect(htmlLang("zh")).toBe("zh-CN")
    applyLocale("en")
    expect(document.documentElement.lang).toBe("en")
  })
})

describe("localizeNotice", () => {
  it("translates chrome notices and leaves model text alone", () => {
    expect(localizeNotice("Standing objective set.", "zh")).toBe("已设置目标。")
    expect(localizeNotice("Standing objective set.", "en")).toBe(
      "Standing objective set.",
    )
    expect(localizeNotice("Memory review failed: boom", "zh")).toBe(
      "记忆复盘失败：boom",
    )
    expect(localizeNotice("Compressing conversation context…", "zh")).toBe(
      "正在压缩会话上下文…",
    )
    expect(localizeNotice("Planning.", "zh")).toBe("正在规划。")
    expect(localizeNotice("Plan updated.", "zh")).toBe("计划已更新。")
    expect(localizeNotice("Retrying after a model error.", "zh")).toBe(
      "模型出错，正在重试。",
    )
    expect(
      localizeNotice(
        "Stopped auto-continuing: the last continuation made no progress.",
        "zh",
      ),
    ).toBe("已停止自动续跑：上一轮续跑没有实质进展。")
    expect(localizeNotice("Work session ended after the time cap.", "zh")).toBe(
      "工作会话因时长上限结束。",
    )
    expect(
      localizeNotice("Work session ended after the tool-round cap.", "zh"),
    ).toBe("工作会话因工具回合上限结束。")
    expect(
      localizeNotice(
        "Context compressed (91200 → 1400 tokens). The transcript is unchanged.",
        "zh",
      ),
    ).toBe("已压缩上下文（91200 → 1400 tokens）。你看到的记录不变。")
    expect(localizeNotice("a model wrote this", "zh")).toBe("a model wrote this")
  })
})

describe("toggleLocalePref", () => {
  it("pins the other language, never system", () => {
    expect(toggleLocalePref("en")).toBe("zh")
    expect(toggleLocalePref("zh")).toBe("en")
    expect(toggleLocalePref("system", "en-US")).toBe("zh")
    expect(toggleLocalePref("system", "zh-CN")).toBe("en")
  })
})
