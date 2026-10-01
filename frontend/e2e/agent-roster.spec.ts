import { expect, test } from "@playwright/test"

import { freshConversation, send, waitForIdle } from "./session"

test("the agent roster sorts by last update and by name", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  await waitForIdle(page)
  await page.getByRole("tab", { name: "Agents" }).click()

  const roster = page.getByTestId("agent-roster")
  await expect(page.getByRole("combobox", { name: "Sort by" })).toContainText("Last update")
  await expect(page.getByRole("button", { name: "Newest first" })).toBeVisible()

  await page.getByRole("combobox", { name: "Sort by" }).click()
  await page.getByRole("option", { name: "Name" }).click()
  await expect(page.getByRole("button", { name: "Z to A" })).toBeVisible()
  await page.getByRole("button", { name: "Z to A" }).click()
  await expect(page.getByRole("button", { name: "A to Z" })).toBeVisible()

  const ids = await roster.locator("[data-agent-id]").evaluateAll((els) =>
    els.map((el) => el.getAttribute("data-agent-id") ?? ""),
  )
  const research = ids.findIndex((id) => id.startsWith("researcher"))
  const review = ids.findIndex((id) => id.startsWith("reviewer"))
  expect(research).toBeGreaterThanOrEqual(0)
  expect(review).toBeGreaterThan(research)

  await page.getByRole("combobox", { name: "Sort by" }).click()
  await page.getByRole("option", { name: "Created" }).click()
  await expect(page.getByRole("button", { name: "Oldest first" })).toBeVisible()
  await page.getByRole("button", { name: "Oldest first" }).click()
  await expect(page.getByRole("button", { name: "Newest first" })).toBeVisible()
})
