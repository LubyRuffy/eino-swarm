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

  const empty = path.join(root, "empty")
  fs.mkdirSync(empty, { recursive: true })
  expect((await page.request.put("/api/settings", {
    data: { clients: { enabled: true, claude_dir: root, codex_dir: empty, cursor_dir: empty } },
  })).ok()).toBeTruthy()
  await page.goto("/")

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

test("an unchanged archive stays readable and changed local sessions refresh", async ({ page }) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "zwai-e2e-client-archive-"))
  const dir = path.join(root, "projects", "work")
  const empty = path.join(root, "empty")
  fs.mkdirSync(dir, { recursive: true })
  fs.mkdirSync(empty, { recursive: true })
  const now = Date.now()
  for (let i = 0; i < 600; i++) {
    const file = path.join(dir, `${i}.jsonl`)
    fs.writeFileSync(file, `${JSON.stringify({ type: "user", message: { content: `archive row ${i}` } })}\n`)
    const at = new Date(now - (600 - i) * 1000)
    fs.utimesSync(file, at, at)
  }
  try {
    expect((await page.request.put("/api/settings", {
      data: { clients: { enabled: true, claude_dir: root, codex_dir: empty, cursor_dir: empty } },
    })).ok()).toBeTruthy()
    await page.goto("/")
    await page.getByTestId("dest-clients").click()
    await expect(page.getByRole("button", { name: /archive row 599$/ })).toBeVisible()
    const catalog = await page.request.get("/api/clients")
    expect(catalog.ok()).toBeTruthy()
    const first = (await catalog.json()).tools[0]
    expect(first.tasks).toHaveLength(5)
    expect(first.more).toBe(true)
    const older = await page.request.get(`/api/clients?before=${first.next}`)
    expect((await older.json()).tools[0].tasks[0].title).toBe("archive row 594")
    // Touch one record, then delete it: each refresh must update the snapshot.
    const newest = path.join(dir, "599.jsonl")
    fs.writeFileSync(newest, `${JSON.stringify({ type: "user", message: { content: "changed archive row" } })}\n`)
    await expect(page.getByRole("button", { name: /changed archive row$/ })).toBeVisible()
    fs.unlinkSync(newest)
    await expect(page.getByRole("button", { name: /changed archive row$/ })).toHaveCount(0)
    await expect(page.getByRole("button", { name: /archive row 598$/ })).toBeVisible()
    await page.screenshot({ path: test.info().outputPath("clients-archive.png") })
  } finally {
    fs.rmSync(root, { recursive: true, force: true })
  }
})
