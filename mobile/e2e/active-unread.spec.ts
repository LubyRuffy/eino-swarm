import { expect, test } from "@playwright/test"

test("active identities hide old completions without reading them or losing More", async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem("zwai.phone.locale", "en"))
  await page.goto("/?mock=1&tick=0&history=active-unread")
  const attention = page.getByTestId("completion-attention")
  const open = (title: string) => attention.getByRole("button", { name: `Open ${title}`, exact: true })
  await expect(page.getByTestId("inbox-list").getByText("In progress", { exact: true })).toBeVisible()
  await expect(open("Shared task")).toHaveCount(1)
  await expect(open("Pending answer")).toHaveCount(0)
  await expect(open("Scheduled task")).toHaveCount(0)
  await expect(attention.getByTestId("row-state")).toHaveText(["Unread completion"])
  await page.screenshot({ path: info.outputPath("active-unread-first-page.png"), animations: "disabled" })

  await attention.getByRole("button", { name: "More", exact: true }).click()
  await expect(open("Completed item 2")).toBeVisible()
  await expect(open("Shared task")).toHaveCount(1)
  await expect(open("Pending answer")).toHaveCount(0)
  await expect(open("Scheduled task")).toHaveCount(0)
  await page.waitForTimeout(2200)
  await expect(open("Completed item 2")).toBeVisible()
  await page.screenshot({ path: info.outputPath("active-unread-expanded-live.png"), animations: "disabled" })

  await attention.getByRole("button", { name: "More", exact: true }).click()
  // The host changes only live status. A phone read ACK would remove these
  // old completion watermarks instead of making them visible again.
  await expect(open("Shared task")).toHaveCount(2, { timeout: 15_000 })
  await expect(open("Pending answer")).toHaveCount(1)
  await expect(open("Scheduled task")).toHaveCount(1)
  await expect(open("Completed item 4")).toBeVisible()
  await expect(attention.getByRole("button", { name: "More", exact: true })).toHaveCount(0)
  await expect(attention.getByTestId("row-state")).toHaveCount(8)
  await expect(page.getByRole("button", { name: "Back", exact: true })).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("active-unread-idle-watermarks-retained.png"), animations: "disabled" })
})
