import { readFile } from "node:fs/promises"
import { expect, test } from "@playwright/test"

test("a previewed file downloads with its original bytes", async ({ page }) => {
  await page.goto("/?mock=1&tick=0")
  await page.getByRole("button", { name: "产出物", exact: true }).click()
  await page.getByRole("button", { name: "outputs", exact: true }).click()
  await page.getByRole("button", { name: "report.md", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Verified output", level: 1 })).toBeVisible()
  const downloading = page.waitForEvent("download")
  await page.getByRole("link", { name: "下载", exact: true }).click()
  const download = await downloading
  expect(download.suggestedFilename()).toBe("report.md")
  const path = await download.path()
  expect(path).not.toBeNull()
  expect(await readFile(path!)).toEqual(Buffer.from("# Verified output\n\n> A finding\n\n- First result\n"))
  await expect(page.getByTestId("phone-file-preview")).toBeVisible()
})
