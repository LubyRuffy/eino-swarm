import fs from "node:fs"
import os from "node:os"
import path from "node:path"

import { expect, test } from "@playwright/test"

test("a long local agent session follows the latest lines and Earlier loads the rest", async ({
  page,
}) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "zwai-e2e-client-"))
  const dir = path.join(root, "projects", "work")
  fs.mkdirSync(dir, { recursive: true })
  const lines = [
    JSON.stringify({
      type: "user",
      message: { content: [{ type: "text", text: "paged request" }] },
    }),
  ]
  for (let i = 0; i < 80; i++) {
    lines.push(
      JSON.stringify({
        type: "assistant",
        message: { content: [{ type: "text", text: `earlier line ${i}` }] },
      }),
    )
  }
  lines.push(
    JSON.stringify({
      type: "assistant",
      message: { content: [{ type: "text", text: "latest reply" }] },
    }),
  )
  fs.writeFileSync(path.join(dir, "s1.jsonl"), `${lines.join("\n")}\n`)

  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Clients" }).click()
  const empty = path.join(root, "empty")
  fs.mkdirSync(empty, { recursive: true })
  await dialog.getByLabel("Claude directory").fill(root)
  await dialog.getByLabel("Codex directory").fill(empty)
  await dialog.getByLabel("Cursor directory").fill(empty)
  await dialog.getByLabel("Show local agent tasks").click()
  await dialog.getByRole("button", { name: "Back to app" }).click()

  await expect(page.getByTestId("dest-clients")).toBeVisible()
  await page.getByTestId("dest-clients").click()
  await page.getByRole("button", { name: "paged request" }).click()
  await expect(page.getByTestId("client-chat")).toBeVisible()
  await expect(page.getByText("latest reply")).toBeVisible()
  await expect(page.getByText("earlier line 0")).toHaveCount(0)
  await page.getByTestId("client-earlier").click()
  await expect(page.getByText("earlier line 0")).toBeVisible()
  await expect(page.getByText("latest reply")).toBeVisible()
})
