import { readFileSync } from "node:fs"

import { expect, test } from "@playwright/test"

import { RELEASE_REPO, RELEASES_LATEST_URL } from "../src/lib/app-update"

const shell = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")) as {
  version: string
}

function bump(version: string): string {
  const [major, minor, patch] = version.split(".").map(Number)
  return `${major}.${minor}.${patch + 1}`
}

function release(version: string) {
  return {
    tag_name: "v" + version,
    html_url: "https://github.com/" + RELEASE_REPO + "/releases/tag/v" + version,
    draft: false,
    prerelease: false,
    assets: [
      {
        name: "zwai-" + version + "-android.apk",
        browser_download_url:
          "https://github.com/" +
          RELEASE_REPO +
          "/releases/download/v" +
          version +
          "/zwai-" +
          version +
          "-android.apk",
      },
    ],
  }
}

test("the menu check shows progress, then latest, an update, or the feed error", async ({ page }) => {
  let mode: "update" | "current" | "error" = "update"
  await page.route(RELEASES_LATEST_URL, async (route) => {
    if (mode === "error") {
      await route.fulfill({
        status: 403,
        contentType: "application/json",
        body: JSON.stringify({ message: "feed refused the check" }),
      })
      return
    }
    if (mode === "update") await new Promise((r) => setTimeout(r, 400))
    const version = mode === "current" ? shell.version : bump(shell.version)
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(release(version)),
    })
  })

  await page.goto("/?mock=1&tick=0")
  await page.getByRole("button", { name: "返回" }).click()
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "检查新版本" }).click()

  await expect(page.getByRole("status")).toHaveText("正在获取版本")
  await expect(page.getByRole("progressbar")).toBeVisible()
  await expect(page.getByRole("status")).toHaveText(`有新版本 ${bump(shell.version)}，是否升级？`)
  await expect(page.getByRole("button", { name: "更新" })).toBeVisible()
  await page.getByRole("dialog", { name: "检查新版本" }).getByRole("button", { name: "以后再说" }).click()
  await expect(page.getByRole("dialog", { name: "检查新版本" })).toHaveCount(0)

  mode = "error"
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "检查新版本" }).click()
  await expect(page.getByRole("alert")).toHaveText("feed refused the check")
  await expect(page.getByRole("button", { name: "更新" })).toHaveCount(0)
  await page.getByRole("dialog", { name: "检查新版本" }).getByRole("button", { name: "关闭" }).click()

  mode = "current"
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "检查新版本" }).click()
  await expect(page.getByRole("status")).toHaveText("已经是最新版本")
  await expect(page.getByRole("button", { name: "更新" })).toHaveCount(0)
})
