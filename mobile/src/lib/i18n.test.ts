import { describe, expect, it } from "vitest"

import { en } from "./messages-en"
import { zh } from "./messages-zh"
import {
  getLocale,
  localeSwitchLabel,
  resolveLocale,
  setLocale,
  t,
  toggleLocale,
} from "./i18n"

describe("i18n", () => {
  it("has a Chinese string for every English key", () => {
    const missing = (Object.keys(en) as Array<keyof typeof en>).filter((k) => !zh[k])
    expect(missing).toEqual([])
  })

  it("follows zh* as Chinese", () => {
    expect(resolveLocale("zh-CN")).toBe("zh")
    expect(resolveLocale("en-US")).toBe("en")
  })

  it("toggles and interpolates nothing extra", () => {
    setLocale("en")
    expect(t("scan.camera")).toBe("Scan QR")
    expect(toggleLocale()).toBe("zh")
    expect(t("scan.camera")).toBe("扫描二维码")
    expect(getLocale()).toBe("zh")
    expect(localeSwitchLabel()).toBe("EN")
    setLocale("en")
    expect(localeSwitchLabel()).toBe("中文")
  })
})
