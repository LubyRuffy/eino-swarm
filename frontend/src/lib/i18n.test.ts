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
    expect(t("en", "tool.diffMore", { n: 12 })).toBe("12 more lines")
    expect(t("zh", "tool.diffMore", { n: 12 })).toBe("还有 12 行")
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
      localizeNotice("The model returned a tool call that was not valid JSON. Retrying.", "zh"),
    ).toBe("模型返回的工具调用不是合法 JSON，正在重试。")
    expect(localizeNotice("The model service returned a response that could not be decoded. Retrying.", "zh")).toBe("模型服务返回的响应无法解析，正在重试。")
    expect(localizeNotice("the model service returned a response that could not be decoded", "zh")).toBe("模型服务返回的响应无法解析。")
    expect(localizeNotice("The model service is unavailable. Retrying.", "zh")).toBe(
      "模型服务不可用，正在重试。",
    )
    expect(localizeNotice("The model service is rate limiting requests. Retrying.", "zh")).toBe(
      "模型服务正在限流，正在重试。",
    )
    expect(localizeNotice("The connection to the model service dropped. Retrying.", "zh")).toBe(
      "与模型服务的连接中断，正在重试。",
    )
    expect(localizeNotice("The model service stopped sending data. Retrying.", "zh")).toBe(
      "模型服务停止发送数据，正在重试。",
    )
    expect(localizeNotice("the model returned a tool call that was not valid JSON", "zh")).toBe(
      "模型返回的工具调用不是合法 JSON。",
    )
    expect(
      localizeNotice("the model request failed because a tool call was not valid JSON", "zh"),
    ).toBe("模型返回的工具调用不是合法 JSON。")
    expect(localizeNotice("the model service is rate limiting requests", "zh")).toBe(
      "模型服务正在限流。",
    )
    expect(localizeNotice("the model service is unavailable", "zh")).toBe("模型服务不可用。")
    expect(localizeNotice("the connection to the model service dropped", "zh")).toBe(
      "与模型服务的连接中断。",
    )
    expect(
      localizeNotice("The prompt exceeds the model window. Compressing and trying once.", "zh"),
    ).toBe("上下文超过模型窗口。先压缩，再试一次。")
    expect(
      localizeNotice(
        "the prompt plus the requested output exceeds this model's context window; automatic compact could not free enough space. Lower Max completion tokens in Settings → Swarm, or shorten the request, then retry",
        "zh",
      ),
    ).toBe(
      "上下文超限：输入加上请求的输出超过了模型窗口。自动压缩腾不出足够空间。请到设置 → Swarm 降低最大补全 token，或缩短请求后再试。",
    )
    expect(
      localizeNotice(
        "the prompt plus the requested output exceeds this model's context window; automatic compact could not free enough space. Lower Max completion tokens in Settings → Swarm, or shorten the request, then retry. The scheduled check is paused",
        "zh",
      ),
    ).toBe(
      "上下文超限：输入加上请求的输出超过了模型窗口。自动压缩腾不出足够空间。请到设置 → Swarm 降低最大补全 token，或缩短请求后再试。定时检查已暂停，恢复之前不会再自动跑。",
    )
    expect(localizeNotice("Continuing the scheduled check.", "zh")).toBe("正在继续执行定时检查。")
    expect(
      localizeNotice(
        "Stopped auto-continuing: the last continuation made no progress. Press Start on the goal to keep going.",
        "zh",
      ),
    ).toBe("已停止自动续跑：上一轮续跑没有实质进展。点「开始目标」继续。")
    expect(
      localizeNotice(
        "Stopped auto-continuing: the standing objective is still open. Press Start on the goal to keep going.",
        "zh",
      ),
    ).toBe("已停止自动续跑：目标仍未完成。点「开始目标」继续。")
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
    expect(
      localizeNotice("Standing objective paused. Press Start on the goal to keep going.", "zh"),
    ).toBe("目标已暂停。点「开始目标」继续。")
    expect(localizeNotice("a model wrote this", "zh")).toBe("a model wrote this")
    expect(localizeNotice("A wait is armed.", "zh")).toBe("已设置等待。")
    expect(localizeNotice("A wait was cancelled.", "zh")).toBe("等待已取消。")
    expect(localizeNotice("Scheduled check.", "zh")).toBe("定时检查。")
    expect(localizeNotice("Saved to the skill library.", "zh")).toBe("已写入共享技能库。")
    expect(
      localizeNotice('Saved to the skill library: skill "a-procedure" recorded.', "zh"),
    ).toBe('已写入共享技能库：skill "a-procedure" recorded。')
  })

  it("keeps schedule chrome generic", () => {
    const blob = `${JSON.stringify(en)}\n${JSON.stringify(zh)}`
    expect(blob).not.toMatch(/GitHub|github\.com/i)
    expect(blob).not.toMatch(/0 \d+ \* \*/)
    expect(blob.toLowerCase()).not.toMatch(/\bdeploy\b/)
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
