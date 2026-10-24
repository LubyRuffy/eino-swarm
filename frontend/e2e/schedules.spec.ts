import { expect, test, type APIRequestContext, type Page } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { reloadOpenConversation } from "./session"

/** Every spec starts on its own conversation, so one failing run cannot leave
 *  state that breaks the next. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

const statusBadge = (page: Page) => page.getByTestId("status-badge")

async function waitForIdle(page: Page) {
  await expect(statusBadge(page)).toContainText("Idle", { timeout: 60_000 })
}

async function waitUntilUnread(request: APIRequestContext) {
  // Standalone Run now mints another conversation. The origin status badge
  // stays Idle, so unread is how we know the mock report finished.
  await expect
    .poll(
      async () => {
        const body = (await (await request.get("/api/schedules")).json()) as {
          unread?: number
        }
        return body.unread ?? 0
      },
      { timeout: 60_000 },
    )
    .toBeGreaterThan(0)
}

async function openInbox(page: Page) {
  await page.getByTestId("schedule-inbox").click()
  await expect(page.getByTestId("schedule-page")).toBeVisible()
  await expect(page.getByTestId("side-panel")).toHaveCount(0)
}

async function closeInbox(page: Page) {
  for (let i = 0; i < 4; i++) {
    if ((await page.getByTestId("schedule-page").count()) === 0) return
    await page.keyboard.press("Escape")
  }
  await expect(page.getByTestId("schedule-page")).toHaveCount(0)
}

async function revealThread(page: Page, id: string) {
  const row = page.locator(`[data-testid="thread-row"][data-id="${id}"]`)
  if ((await row.count()) === 0) {
    const more = page.getByRole("button", { name: "Show more" })
    if ((await more.count()) > 0) await more.click()
  }
  await expect(row).toBeVisible()
  return row
}

test("standalone wait from the inbox runs now and opens findings", async ({
  page,
  request,
}) => {
  let scheduleId = ""
  try {
    test.setTimeout(120_000)
    await freshConversation(page)

    await openInbox(page)
    const inbox = page.getByTestId("schedule-page")
    await inbox.getByRole("button", { name: "New task" }).click()
    await expect(page.getByTestId("schedule-create-drawer")).toBeVisible()
    await inbox.getByLabel("Task").fill("Continue the wait.")
    await inbox.getByLabel("Repeat").click()
    await page.getByRole("option", { name: "On an interval" }).click()
    await inbox.getByLabel("Every (seconds)").fill("60")
    const createdResponse = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/schedules")
    await inbox.getByRole("button", { name: "Add wait" }).click()
    scheduleId = (await (await createdResponse).json()).schedule.id
    await expect(page.getByTestId("schedule-create-drawer")).toHaveCount(0)
    await expect(inbox.getByTestId("schedule-row")).toBeVisible()
    let named = ""
    await expect
      .poll(
        async () => {
          const body = (await (await request.get("/api/schedules")).json()) as {
            schedules?: Array<{ title?: string; title_auto?: boolean }>
          }
          const row = body.schedules?.[0]
          named = (row?.title ?? "").trim()
          return Boolean(row && row.title_auto === false && named)
        },
        { timeout: 30_000 },
      )
      .toBeTruthy()
    await expect(inbox.getByTestId("schedule-row")).toContainText(named)

    await inbox.getByTestId("schedule-row-toggle").click()
    await expect(page.getByTestId("schedule-edit-drawer")).toBeVisible()
    await inbox.getByRole("button", { name: "Run now" }).click()
    // Leave the page so the title-bar Idle badge is readable, then wait for
    // the minted turn via unread.
    await closeInbox(page)
    await waitForIdle(page)
    await waitUntilUnread(request)

    await openInbox(page)
    await expect(page.getByTestId("schedule-unread")).toHaveCount(0)
    const findings = page.getByRole("button", { name: "Open findings" })
    await expect(findings).toBeVisible()
    await findings.click()

    await expect(page.getByTestId("schedule-page")).toHaveCount(0)
    await expect(page.getByTestId("thread-title")).toHaveText(named)
    await expect(
      page.locator('[data-testid="thread-row"][aria-current="true"]'),
    ).toContainText(named)
    await expect(page.getByTestId("schedule-notice")).toContainText("Scheduled check.")
    await expect(page.getByTestId("transcript")).not.toContainText(
      "This turn is a scheduled check.",
    )
    await expect(page.getByTestId("user-message")).toHaveCount(0)
    await expect(page.getByTestId("turn-boundary").locator("time")).toHaveAttribute("datetime", /\d{4}-\d{2}-\d{2}T/)
  } finally {
    // This recurring fixture must not mint new Recents rows during later specs.
    if (scheduleId) expect((await request.delete(`/api/schedules/${scheduleId}`)).ok()).toBe(true)
  }
})

test("a thread wake from REST shows a banner that cancel removes", async ({
  page,
  request,
}) => {
  await freshConversation(page)

  const listed = (await (await request.get("/api/threads")).json()) as {
    threads: { id: string; last_active_at?: string }[]
  }
  const activeId = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  const newest = [...(listed.threads ?? [])].sort(
    (a, b) =>
      Date.parse(b.last_active_at ?? "") - Date.parse(a.last_active_at ?? ""),
  )[0]
  const threadId = listed.threads.find((th) => th.id === activeId)?.id ?? newest?.id
  expect(threadId).toBeTruthy()

  const created = await request.post("/api/schedules", {
    data: {
      kind: "thread",
      thread_id: threadId,
      prompt: "Continue the wait.",
      every_s: 60,
    },
  })
  expect(created.status()).toBe(201)

  await page.reload()
  await expect(page.getByTestId("composer-input")).toBeVisible()
  const row = await revealThread(page, threadId!)
  if ((await row.getAttribute("aria-current")) !== "true") {
    await row.getByTestId("row-label").click()
  }

  await expect(page.getByTestId("schedule-banner")).toBeVisible()
  await expect(
    page.getByTestId("schedule-banner").getByRole("button", { name: "Run now" }),
  ).toBeVisible()
  await expect(statusBadge(page)).toContainText("Waiting")
  await expect(row.getByTestId("wait-mark")).toBeVisible()
  await page
    .getByTestId("schedule-banner")
    .getByRole("button", { name: "Cancel wait" })
    .click()
  await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Idle")
  await expect(row.getByTestId("wait-mark")).toHaveCount(0)
})

test("a thread wake banner can run now instead of waiting", async ({
  page,
  request,
}) => {
  test.setTimeout(120_000)
  await freshConversation(page)

  const listed = (await (await request.get("/api/threads")).json()) as {
    threads: { id: string; last_active_at?: string }[]
  }
  const activeId = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  const newest = [...(listed.threads ?? [])].sort(
    (a, b) =>
      Date.parse(b.last_active_at ?? "") - Date.parse(a.last_active_at ?? ""),
  )[0]
  const threadId = listed.threads.find((th) => th.id === activeId)?.id ?? newest?.id
  expect(threadId).toBeTruthy()

  const created = await request.post("/api/schedules", {
    data: {
      kind: "thread",
      thread_id: threadId,
      prompt: "Continue the wait.",
      every_s: 60,
    },
  })
  expect(created.status()).toBe(201)
  const { schedule } = await created.json()
  try {
    await page.reload()
    await expect(page.getByTestId("composer-input")).toBeVisible()
    const row = await revealThread(page, threadId!)
    if ((await row.getAttribute("aria-current")) !== "true") {
      await row.getByTestId("row-label").click()
    }

    await expect(page.getByTestId("schedule-banner")).toBeVisible()
    await expect(statusBadge(page)).toContainText("Waiting")
    await expect(row.getByTestId("wait-mark")).toBeVisible()
    await page
      .getByTestId("schedule-banner")
      .getByRole("button", { name: "Run now" })
      .click()
    await expect(statusBadge(page)).toContainText("Working")
    await expect(page.getByTestId("schedule-banner")).toHaveCount(0)
    await expect(
      page.getByTestId("schedule-notice").filter({ hasText: "Scheduled check." }),
    ).toBeVisible()
    await expect(statusBadge(page)).toContainText("Waiting", { timeout: 60_000 })
    await expect(page.getByTestId("schedule-banner")).toBeVisible()
    await expect(row.getByTestId("wait-mark")).toBeVisible()
  } finally {
    expect((await request.delete(`/api/schedules/${schedule.id}`)).ok()).toBe(true)
  }
})

test("create drawer expand fills the scheduled page", async ({ page }) => {
  await freshConversation(page)
  await openInbox(page)
  const inbox = page.getByTestId("schedule-page")
  await inbox.getByRole("button", { name: "New task" }).click()
  const drawer = page.getByTestId("schedule-create-drawer")
  await expect(page.getByTestId("schedule-list-pane")).toBeVisible()
  await drawer.getByRole("button", { name: "Expand" }).click()
  await expect(drawer).toHaveAttribute("data-expanded", "true")
  await expect(page.getByTestId("schedule-list-pane")).toBeHidden()
  await inbox.getByLabel("Task").fill("Continue the wait.")
  await drawer.getByRole("button", { name: "Collapse" }).click()
  await expect(drawer).not.toHaveAttribute("data-expanded", "true")
  await expect(page.getByTestId("schedule-list-pane")).toBeVisible()
})

test("inbox editor can move a wait onto an existing conversation", async ({ page, request }) => {
  const createdThread = await request.post("/api/threads", {
    data: { title: "Kept chat" },
  })
  expect(createdThread.ok()).toBeTruthy()
  const threadID = ((await createdThread.json()) as { thread: { id: string } }).thread.id

  await page.goto("/")
  const created = await request.post("/api/schedules", {
    data: {
      kind: "standalone",
      title: "Retarget wait",
      prompt: "Continue the wait.",
      every_s: 3600,
    },
  })
  expect(created.status()).toBe(201)
  const before = ((await created.json()) as { schedule: { id: string; next_run_at: string } }).schedule

  await openInbox(page)
  const inbox = page.getByTestId("schedule-page")
  await inbox
    .getByTestId("schedule-row")
    .filter({ hasText: "Retarget wait" })
    .getByTestId("schedule-row-toggle")
    .click()
  await inbox.getByLabel("Runs in").click()
  await page.getByLabel("Search chats").fill("Kept")
  await page.getByRole("option", { name: "Kept chat" }).click()
  await expect(inbox.getByRole("button", { name: "Runs in" })).toHaveText("Kept chat")
  await expect(page.getByRole("combobox", { name: "Project", exact: true })).toHaveCount(0)
  await inbox.getByRole("button", { name: "Save" }).click()
  await expect
    .poll(async () => {
      const body = (await (await request.get(`/api/schedules/${before.id}`)).json()) as {
        schedule?: { kind?: string; thread_id?: string; next_run_at?: string; every_s?: number }
      }
      const row = body.schedule
      return (
        row?.kind === "thread" &&
        row.thread_id === threadID &&
        row.next_run_at === before.next_run_at &&
        row.every_s === 3600
      )
    })
    .toBeTruthy()
})

test("inbox editor patches title, prompt, and cadence", async ({ page, request }) => {
  await freshConversation(page)
  const created = await request.post("/api/schedules", {
    data: {
      kind: "standalone",
      title: "Periodic check",
      prompt: "Continue the wait.",
      every_s: 60,
    },
  })
  expect(created.status()).toBe(201)
  const { schedule } = await created.json()
  try {
    await openInbox(page)
    const inbox = page.getByTestId("schedule-page")
    await inbox
      .getByTestId("schedule-row")
      .filter({ hasText: "Periodic check" })
      .getByTestId("schedule-row-toggle")
      .click()
    const drawer = page.getByTestId("schedule-edit-drawer")
    await expect(drawer).toBeVisible()
    await inbox.getByLabel("Title").fill("Renamed wait")
    await inbox.getByLabel("Task").fill("Check again.")
    await inbox.getByLabel("Every (seconds)").fill("90")
    await inbox.getByRole("button", { name: "Save" }).click()
    await expect
      .poll(
        async () => {
          const body = (await (await request.get("/api/schedules")).json()) as {
            schedules?: Array<{ title?: string; prompt?: string; every_s?: number }>
          }
          const row = (body.schedules ?? []).find((s) => s.title === "Renamed wait")
          return row?.prompt === "Check again." && row?.every_s === 90
        },
        { timeout: 15_000 },
      )
      .toBeTruthy()
    await expect(
      inbox.getByTestId("schedule-row").filter({ hasText: "Renamed wait" }),
    ).toBeVisible()
  } finally {
    expect((await request.delete(`/api/schedules/${schedule.id}`)).ok()).toBe(true)
  }
})

for (const trigger of ["Run now", "Automatic fire"] as const) {
  test(`${trigger} accepts a text result without inventing tool activity`, async ({ page, request }, info) => {
    const created = await request.post("/api/schedules", { data: {
      kind: "standalone", title: "Completion evidence", prompt: "Check the current state.",
      ...(trigger === "Run now" ? { every_s: 3600 } : { delay_s: 30 }),
    } })
    expect(created.status()).toBe(201)
    const { schedule } = await created.json()
    try {
      await page.goto("/")
      await openInbox(page)
      const row = page.getByTestId("schedule-row").filter({ hasText: "Completion evidence" })
      await row.getByTestId("schedule-row-toggle").click()
      let acceptedTurnID = ""
      if (trigger === "Run now") {
        const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/schedules/${schedule.id}/run`)
        await page.getByRole("button", { name: "Run now", exact: true }).click()
        acceptedTurnID = (await (await accepted).json()).turn.id
      }
      let completed: { id: string; turn_id: string; thread_id: string; status: string; summary: string } | undefined
      await expect.poll(async () => {
        const body = await (await request.get(`/api/schedules/${schedule.id}`)).json()
        completed = body.runs[0]
        return completed?.status
      }, { timeout: 60_000 }).toBe("findings")
      const body = await (await request.get(`/api/schedules/${schedule.id}`)).json()
      expect(body.runs).toHaveLength(1)
      if (acceptedTurnID) expect(completed!.turn_id).toBe(acceptedTurnID)
      const trace = await (await request.get(`/api/trace/${completed!.turn_id}`)).json()
      expect(trace.turn).toMatchObject({
        id: completed!.turn_id, thread_id: completed!.thread_id,
        schedule_run_id: completed!.id, status: "done", final: completed!.summary,
      })
      expect(trace.turn.final.trim()).not.toBe("")
      const turns = await (await request.get(`/api/threads/${completed!.thread_id}/turns`)).json()
      expect(turns.turns).toHaveLength(1)
      const events = trace.events as Array<{ kind: string; turn_id: string; thread_id: string; agent_id?: string }>
      expect(events.every(ev => ev.turn_id === completed!.turn_id && ev.thread_id === completed!.thread_id)).toBe(true)
      expect(events.filter(ev => ["model_retry", "tool_call", "tool_result", "schedule_report", "spawned", "error"].includes(ev.kind))).toHaveLength(0)
      expect(events.filter(ev => ev.kind === "agent_message" && ev.agent_id === "manager")).toHaveLength(1)
      expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "manager")).toHaveLength(1)
      await page.getByRole("button", { name: "Open findings", exact: true }).click()
      const transcript = page.getByTestId("transcript")
      await expect(transcript).toContainText(trace.turn.final)
      await expect(transcript.getByTestId("assistant-message")).toHaveCount(1)
      await expect(transcript).not.toContainText("scheduled check ended without")
      await expect(transcript).not.toContainText("Continuing the scheduled check.")
      await expect(transcript).not.toContainText("Retrying after a model error.")
      await expect(transcript.getByRole("button", { name: /^report_schedule/ })).toHaveCount(0)
      await page.screenshot({ path: info.outputPath("text-result.png"), animations: "disabled" })
      await reloadOpenConversation(page)
      await expect(transcript.getByTestId("assistant-message")).toHaveCount(1)
      await expect(transcript).toContainText(trace.turn.final)
      await expect(transcript).not.toContainText("scheduled check ended without")
      await writeFile(info.outputPath("text-result-evidence.json"), JSON.stringify({ schedule, completed, trace, turns }, null, 2), { mode: 0o600 })
    } finally {
      expect((await request.delete(`/api/schedules/${schedule.id}`)).ok()).toBe(true)
    }
  })
}

test("conversation rail opens the wait bound to this chat", async ({ page, request }) => {
  await freshConversation(page)
  const threadId = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  expect(threadId).toBeTruthy()
  const created = await request.post("/api/schedules", {
    data: {
      kind: "thread",
      thread_id: threadId,
      title: "Bound check",
      prompt: "Continue the wait.",
      every_s: 3600,
    },
  })
  expect(created.status()).toBe(201)
  const rail = page.getByTestId("conversation-schedules")
  await expect(rail).toBeVisible({ timeout: 15_000 })
  await rail.getByRole("button", { name: /Bound check/ }).click()
  await expect(page.getByTestId("schedule-edit-drawer")).toBeVisible()
  await expect(page.getByLabel("Title")).toHaveValue("Bound check")
})

test("completed one-shot leaves the conversation rail and remains in completed history", async ({ page, request }) => {
  await freshConversation(page)
  const threadId = await page
    .locator('[data-testid="thread-row"][aria-current="true"]')
    .getAttribute("data-id")
  expect(threadId).toBeTruthy()
  const created = await request.post("/api/schedules", { data: {
    kind: "thread", thread_id: threadId, title: "One-shot check",
    prompt: "Check the current state.", delay_s: 3600,
  } })
  expect(created.status()).toBe(201)
  const { schedule } = await created.json()
  try {
    const rail = page.getByTestId("conversation-schedules")
    await expect(rail).toContainText("One-shot check")
    expect((await request.patch(`/api/schedules/${schedule.id}`, {
      data: { status: "paused" },
    })).ok()).toBe(true)
    await expect(rail).toContainText("Paused")
    await expect(rail).not.toContainText("Next run now")
    expect((await request.patch(`/api/schedules/${schedule.id}`, {
      data: { status: "active" },
    })).ok()).toBe(true)
    await page.getByTestId("schedule-banner").getByRole("button", { name: "Run now" }).click()
    await expect.poll(async () => {
      const body = await (await request.get(`/api/schedules/${schedule.id}`)).json()
      return [body.schedule.status, body.runs[0]?.status]
    }).toEqual(["done", "findings"])
    await expect(rail).toHaveCount(0)
    await waitForIdle(page)

    // A reload must derive the same view from persisted history, rather than
    // only hiding a row locally when the terminal event arrives.
    await page.reload()
    const row = await revealThread(page, threadId!)
    await row.getByTestId("row-label").click()
    await expect(page.getByTestId("composer-input")).toBeVisible()
    await expect(page.getByTestId("conversation-schedules")).toHaveCount(0)
    await openInbox(page)
    await page.getByTestId("schedule-filter-menu").click()
    await page.getByTestId("schedule-filter-completed").click()
    const completed = page.getByTestId("schedule-row").filter({ hasText: "One-shot check" })
    await expect(completed).toContainText("Done")
    await expect(completed).not.toContainText("Next run now")
  } finally {
    expect((await request.delete(`/api/schedules/${schedule.id}`)).ok()).toBe(true)
  }
})
