import { expect, test } from "@playwright/test"

import { freshConversation } from "./session"

test("a switched-off provider leaves the composer model list", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [
          provider,
          {
            id: "debug",
            label: "Debug bench",
            base_url: "http://127.0.0.1:9/v1",
            model: "probe",
            catalog: ["probe"],
            timeout_seconds: 300,
            enabled: true,
          },
        ],
      },
    },
  })
  try {
    await freshConversation(page)
    const picker = page.getByLabel("Model")
    await picker.click()
    await page.getByRole("option", { name: /probe/ }).click()
    await expect(picker).toContainText("probe")

    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Models" }).click()
    await dialog
      .getByRole("switch", { name: "Show Debug bench in the composer" })
      .click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    await expect(picker).toContainText("probe")
    await picker.click()
    await expect(page.getByRole("option", { name: /probe/ })).toHaveCount(0)
    await page.keyboard.press("Escape")

    await page.getByRole("button", { name: "Settings" }).click()
    const again = page.getByRole("dialog")
    await again.getByRole("tab", { name: "Models" }).click()
    await again
      .getByRole("switch", { name: "Show Debug bench in the composer" })
      .click()
    await again.getByRole("button", { name: "Back to app" }).click()
    await expect(again).toBeHidden()
    await picker.click()
    await expect(page.getByRole("option", { name: /probe/ })).toBeVisible()
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("settings omit local agent tasks and the config path", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByTestId("clients-list")).toHaveCount(0)
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("tab", { name: "Clients" })).toHaveCount(0)
  await expect(dialog.getByLabel("Show local agent tasks")).toHaveCount(0)
  await expect(dialog.getByText(/Stored in/)).toHaveCount(0)
  await dialog.getByRole("button", { name: "Back to app" }).click()
})

test("semantic search is off until a model is pinned", async ({ page, request }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models" }).click()
  await expect(dialog.getByRole("switch", { name: "Semantic search", exact: true })).toHaveCount(0)
  await expect(dialog.getByLabel("Embedding model Provider", { exact: true })).toContainText("off")
  await expect(dialog.getByLabel("Embedding model Model", { exact: true })).toHaveCount(0)
  expect((await (await request.get("/api/search?q=configuration-probe")).json()).embedding).toBe(false)
})

// A config that has never been saved from the UI is the state every install
// starts in, and it used to crash the Tools tab: Go marshals an empty exception
// list as null, and the tab read it as an array. Saving anything hid the bug,
// so this checks the wire shape as well as the render.
test("the tool catalogue renders on a config that was never saved", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  expect(Array.isArray(settings.tools.disabled)).toBe(true)
  expect(Array.isArray(settings.tools.enabled)).toBe(true)

  const crashes: string[] = []
  page.on("pageerror", (e) => crashes.push(e.message))

  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Tools" }).click()

  const tools = dialog.getByRole("tabpanel")
  await expect(tools.getByText("read", { exact: true })).toBeVisible()
  // every group is reachable, including the one furthest down
  await expect(tools.getByText("web_search", { exact: true })).toBeVisible()
  await expect(dialog.getByLabel("Skip the proxy for")).toBeVisible()
  await expect(tools.getByText("EASM portrait lookups", { exact: true })).toHaveCount(0)
  await expect(tools.getByLabel("EASM MCP server", { exact: true })).toHaveCount(0)
  await dialog.getByRole("button", { name: "Back to app" }).click()

  expect(crashes).toEqual([])
})

test("settings round-trip through the config file", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText("Data directory", { exact: true })).toBeVisible()
  await expect(dialog.getByText(/Stored in/)).toHaveCount(0)

  await dialog.getByRole("tab", { name: "Swarm" }).click()
  const concurrency = dialog.getByLabel("Sub-agents at once")
  await concurrency.fill("3")
  await dialog.getByRole("button", { name: "Back to app" }).click()
  await expect(dialog).toBeHidden()

  // reopening reads it back from the server, not from React state
  await page.getByRole("button", { name: "Settings" }).click()
  await page.getByRole("dialog").getByRole("tab", { name: "Swarm" }).click()
  await expect(
    page.getByRole("dialog").getByLabel("Sub-agents at once"),
  ).toHaveValue("3")

  // and the tool catalogue is listed with a switch per tool
  await page.getByRole("dialog").getByRole("tab", { name: "Tools" }).click()
  const tools = page.getByRole("dialog").getByRole("tabpanel")
  await expect(tools.getByText("read", { exact: true })).toBeVisible()
  expect(await tools.getByRole("switch").count()).toBeGreaterThan(5)
  await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
})

test("an invalid sub-agent cap is never autosaved", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const previous = settings.swarm.max_concurrent as number
  const valid = previous === 2 ? 3 : 2
  const sent: number[] = []
  page.on("request", (outgoing) => {
    if (outgoing.method() !== "PUT" || !outgoing.url().endsWith("/api/settings")) return
    const value = outgoing.postDataJSON()?.swarm?.max_concurrent
    if (typeof value === "number") sent.push(value)
  })

  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Swarm" }).click()
    const cap = dialog.getByRole("spinbutton", { name: "Sub-agents at once" })
    await expect(cap).toHaveValue(String(previous))
    await cap.fill("0")
    await expect(cap).toHaveAttribute("aria-invalid", "true")
    await expect(dialog.getByRole("alert")).toHaveText("Enter a whole number of at least 1.")
    // Closing flushes any pending autosaves and reopening reads the server.
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    expect(sent).toEqual([])
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    await dialog.getByRole("tab", { name: "Swarm" }).click()
    await expect(cap).toHaveValue(String(previous))
    await cap.fill("0")
    await cap.fill(String(valid))
    await expect(cap).not.toHaveAttribute("aria-invalid", "true")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    expect(sent).toEqual([valid])
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.swarm.max_concurrent).toBe(valid)
  } finally {
    const restored = await request.put("/api/settings", { data: { swarm: settings.swarm } })
    expect(restored.ok()).toBe(true)
  }
})

test("per-note memory cap round-trips through settings", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Memory" }).click()
    const cap = dialog.getByLabel("Per-note cap (characters)")
    await expect(cap).toHaveValue(String(settings.memory.entry_max))
    await cap.fill("280")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.memory.entry_max).toBe(280)

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Memory" }).click()
    await expect(
      page.getByRole("dialog").getByLabel("Per-note cap (characters)"),
    ).toHaveValue("280")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", { data: { memory: settings.memory } })
  }
})

test("review thinking saves independently, can inherit and returns to Default", async ({ page, request }, info) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Memory" }).click()
    const thinking = dialog.getByLabel("Review thinking")
    await expect(thinking).toContainText("Default (Low)")
    await thinking.click()
    await page.getByRole("option", { name: "Medium", exact: true }).click()
    await expect(thinking).toContainText("Medium")
    await info.attach("review-thinking-medium", {
      body: await dialog.screenshot(), contentType: "image/png",
    })
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.memory).toEqual({ ...settings.memory, review_reasoning_effort: "medium" })
    expect(saved.settings.swarm).toEqual(settings.swarm)
    expect(saved.settings.models).toEqual(settings.models)

    await page.getByRole("button", { name: "Settings" }).click()
    await dialog.getByRole("tab", { name: "Memory" }).click()
    await expect(thinking).toContainText("Medium")
    await thinking.click()
    await page.getByRole("option", { name: "Inherit conversation", exact: true }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const inheriting = await (await request.get("/api/settings")).json()
    expect(inheriting.settings.memory).toEqual({ ...settings.memory, review_reasoning_effort: "inherit" })

    await page.getByRole("button", { name: "Settings" }).click()
    await dialog.getByRole("tab", { name: "Memory" }).click()
    await thinking.click()
    await page.getByRole("option", { name: "Default (Low)", exact: true }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const restored = await (await request.get("/api/settings")).json()
    expect(restored.settings.memory).toEqual({ ...settings.memory, review_reasoning_effort: "" })
  } finally {
    await request.put("/api/settings", { data: { memory: settings.memory } })
  }
})

test("policy check thinking and the tool result limit save through settings", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    expect(settings.swarm.policy_reasoning_effort).toBe("")
    expect(settings.tools.result_max_chars).toBeGreaterThan(0)
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Swarm" }).click()
    const policy = dialog.getByLabel("Policy check thinking")
    await expect(policy).toContainText("Default (Low)")
    await policy.click()
    await page.getByRole("option", { name: "Inherit conversation", exact: true }).click()
    await dialog.getByRole("tab", { name: "Tools" }).click()
    const limit = dialog.getByLabel("Tool result limit (characters)")
    await expect(limit).toHaveValue(String(settings.tools.result_max_chars))
    await limit.fill("12000")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.swarm).toEqual({ ...settings.swarm, policy_reasoning_effort: "inherit" })
    expect(saved.settings.tools).toEqual({ ...settings.tools, result_max_chars: 12000 })
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm, tools: settings.tools } })
  }
})

test("answer check switch and model round-trip through settings", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    await page.goto("/")
    const open = page.getByRole("button", { name: "Settings" })
    const dialog = page.getByRole("dialog")
    await open.click()
    await dialog.getByRole("tab", { name: "Swarm" }).click()
    const check = dialog.getByRole("switch", { name: "Check scheduling claims in answers" })
    const model = dialog.getByLabel("Answer check model")
    await expect(check).toBeChecked()
    await expect(model).toHaveText("Follow chat model")
    await model.click()
    await page.getByRole("option", { name: "Automatic", exact: true }).click()
    await model.click()
    await page.getByRole("option", { name: "Decision model", exact: true }).click()
    await check.click()
    await expect(model).toBeDisabled()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const { settings: saved } = await (await request.get("/api/settings")).json()
    expect(saved.swarm).toMatchObject({ schedule_answer_check: false, schedule_answer_model: "decision" })

    await page.reload()
    await open.click()
    await dialog.getByRole("tab", { name: "Swarm" }).click()
    await expect(check).not.toBeChecked()
    await expect(model).toHaveText("Decision model")
    await check.click()
    await expect(model).toBeEnabled()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const { settings: restored } = await (await request.get("/api/settings")).json()
    expect(restored.swarm).toMatchObject({ schedule_answer_check: true, schedule_answer_model: "decision" })
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm } })
  }
})

test("personality round-trips through settings", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Personality" }).click()
    const box = dialog.getByLabel("Personal preferences")
    await expect(box).toHaveValue(settings.personality?.instructions ?? "")
    await box.fill("prefer compact replies")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.personality.instructions).toBe(
      "prefer compact replies",
    )

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Personality" }).click()
    await expect(
      page.getByRole("dialog").getByLabel("Personal preferences"),
    ).toHaveValue("prefer compact replies")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", {
      data: { personality: settings.personality ?? { instructions: "" } },
    })
  }
})

test("pins a suggestion model in its drawer when more than one name is listed", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [
          {
            ...provider,
            model: "alpha",
            catalog: ["alpha", "beta"],
          },
        ],
      },
      swarm: { ...settings.swarm, composer_predictions: false },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog", { name: "Settings", exact: true })
    await dialog.getByRole("tab", { name: "Models" }).click()
    const entry = dialog.getByRole("button", { name: "Configure Suggest a next step", exact: true })
    await entry.click()
    const drawer = page.getByRole("dialog", { name: "Suggest a next step", exact: true })
    const picker = drawer.getByRole("combobox", { name: "Model", exact: true })
    await expect(picker).toHaveCount(0)
    await drawer.getByRole("switch", { name: "Suggest a next step", exact: true }).setChecked(true)
    await expect(picker).toContainText("Follow the conversation model")
    await picker.click()
    await page.getByRole("option", { name: "beta", exact: true }).click()
    await expect.poll(async () => {
      const saved = await (await request.get("/api/settings")).json()
      return saved.settings.models.bindings?.composer_prediction
    }).toEqual({ provider_id: provider.id, model: "beta" })
    await drawer.getByRole("button", { name: "Close", exact: true }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    await page.reload()
    await page.getByRole("button", { name: "Settings" }).click()
    await dialog.getByRole("tab", { name: "Models" }).click()
    await entry.click()
    await expect(drawer.getByLabel("Model Model", { exact: true })).toHaveValue("beta")
  } finally {
    await request.put("/api/settings", {
      data: { models: settings.models, swarm: settings.swarm },
    })
  }
})

test("discovers models into the default dropdown", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [{ ...provider, base_url: "http://endpoint.invalid/v1" }],
      },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Models" }).click()
    const heading = (provider.label || provider.id).trim()
    await expect(
      dialog.getByRole("button", { name: `${heading} details` }),
    ).toBeVisible()
    await expect(dialog.getByRole("textbox", { name: "Base URL", exact: true })).toHaveCount(0)
    await dialog.getByRole("button", { name: `${heading} details` }).click()
    await expect(dialog.getByLabel("Provider", { exact: true })).toBeVisible()
    await expect(dialog.getByLabel("Base URL", { exact: true })).toHaveValue(
      "http://endpoint.invalid/v1",
    )
    const ownServices = dialog.locator("section").filter({ has: page.getByRole("heading", { name: "Your own model services", exact: true }) })
    await ownServices.getByRole("button", { name: "Discover models" }).click()
    // Opening the Radix listbox inside a Dialog marks the dialog aria-hidden,
    // so Back to app would hang. The combobox showing the name is enough: that is
    // the control the composer also reads after the sheet writes.
    await expect(
      dialog.getByRole("combobox", { name: "Default model" }),
    ).toContainText("mock")
    await dialog.getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("a failed model listing toasts over the open provider", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1100, height: 700 })
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [{ ...provider, base_url: "http://endpoint.invalid/v1" }],
      },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Models" }).click()
    const heading = (provider.label || provider.id).trim()
    await dialog.getByRole("button", { name: `${heading} details` }).click()
    const ownServices = dialog.locator("section").filter({ has: page.getByRole("heading", { name: "Your own model services", exact: true }) })
    const discover = ownServices.getByRole("button", { name: "Discover models" })
    await expect(discover).toBeEnabled()
    await page.route("**/api/models/discover", async (route) => {
      await route.fulfill({
        status: 502,
        contentType: "application/json",
        body: JSON.stringify({
          error: "provider: list models: connect: no route to host",
        }),
      })
    })
    await discover.click()
    const toast = page.getByRole("alert")
    await expect(toast).toContainText("Couldn't list models")
    await expect(toast).toContainText("connect: no route to host")
    await expect(toast).toBeInViewport()
    await toast.getByRole("button", { name: "Dismiss" }).click()
    await expect(toast).toHaveCount(0)
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("Back to app stays on screen when Swarm is taller than the window", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1100, height: 700 })
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Swarm" }).click()

  const back = dialog.getByRole("button", { name: "Back to app" })
  await expect(back).toBeVisible()
  const box = await back.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height).toBeLessThanOrEqual(700)

  const last = dialog.getByLabel("Policy check thinking")
  await last.scrollIntoViewIfNeeded()
  const lastBox = await last.boundingBox()
  const backBox = await back.boundingBox()
  expect(lastBox).not.toBeNull()
  expect(backBox).not.toBeNull()
  expect(backBox!.y).toBeGreaterThanOrEqual(0)
  expect(backBox!.y + backBox!.height).toBeLessThanOrEqual(700)
  // Back to app lives in the left rail, not in the scrolling page.
  expect(backBox!.y + backBox!.height).toBeLessThanOrEqual(lastBox!.y + 1)
})

test("the Models tab does not clip the add-endpoint button's border", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models" }).click()
  const button = dialog.getByRole("button", { name: "Add a provider" })
  await button.scrollIntoViewIfNeeded()
  const room = await button.evaluate((el) => {
    const panel = el.closest('[role="tabpanel"]')
    if (!(panel instanceof HTMLElement)) return Number.NEGATIVE_INFINITY
    return panel.getBoundingClientRect().bottom - el.getBoundingClientRect().bottom
  })
  // A 1px outline flush with the overflow clip looks like the button has
  // no bottom border. The scrollport keeps padding under the last control.
  expect(room).toBeGreaterThanOrEqual(1)
})
test("switches color palette and remembers it", async ({ page, request }, info) => {
  // Start from zwai even if an earlier crashed run left fofa behind.
  await request.put("/api/settings", { data: { ui: { palette: "zwai" } } })
  await page.emulateMedia({ colorScheme: "light" })
  await page.goto("/")
  const homeHeading = page.getByRole("heading", { name: "What should we work on?" })
  const homeTitle = homeHeading.locator("span")
  await expect(homeTitle).toHaveCSS("background-image", "none")
  // Control radius before the switch, for the component-form comparison below.
  const zwaiControlRadius = parseFloat(
    await page
      .getByRole("button", { name: "Settings" })
      .evaluate((el) => getComputedStyle(el).borderRadius),
  )
  try {
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "General" }).click()
    await expect(dialog.getByRole("radio", { name: "Light" })).toBeVisible()
    await dialog.getByRole("combobox", { name: "Color theme" }).click()
    await page.getByRole("option", { name: "FOFA" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const root = page.locator("html")
    await expect(root).toHaveAttribute("data-palette", "fofa")
    await expect(homeTitle).toHaveCSS("background-clip", "text")
    const vipGradient = /^linear-gradient\(90deg, rgb\(5, 242, 242\)(?: 0%)?, rgb\(59, 130, 246\)(?: 100%)?\)$/
    await expect(homeTitle).toHaveCSS("background-image", vipGradient, { timeout: 3_000 })
    await homeHeading.screenshot({ path: info.outputPath("fofa-home-title-light.png") })
    await page.emulateMedia({ colorScheme: "dark" })
    await expect(root).toHaveClass(/\bdark\b/)
    await expect(homeTitle).toHaveCSS("background-image", vipGradient)
    await homeHeading.screenshot({ path: info.outputPath("fofa-home-title-dark.png") })
    await page.emulateMedia({ colorScheme: "light", forcedColors: "active" })
    await expect(homeTitle).toHaveCSS("background-image", "none")
    await page.emulateMedia({ forcedColors: "none" })
    const tokens = await root.evaluate((el) => {
      const s = getComputedStyle(el)
      return {
        primary: s.getPropertyValue("--primary").trim(),
        background: s.getPropertyValue("--background").trim(),
        foreground: s.getPropertyValue("--foreground").trim(),
        content: s.getPropertyValue("--content-foreground").trim(),
        sidebar: s.getPropertyValue("--sidebar").trim(),
        brandFrom: s.getPropertyValue("--brand-from").trim(),
      }
    })
    expect(tokens.primary.startsWith("180")).toBe(true)
    expect(tokens.brandFrom.startsWith("213")).toBe(true)
    // The FOFA console keeps the rail a step away from the canvas and answers
    // a step below headings. A shared value means the
    // palette regressed to a flat recolor.
    expect(tokens.sidebar).not.toBe(tokens.background)
    expect(tokens.content).not.toBe(tokens.foreground)

    // Stage 2 radius tier: controls sit tighter under fofa, panels wider.
    // Measured on real elements because custom properties keep their calc()
    // expression instead of a resolved length.
    const fofaControlRadius = parseFloat(
      await page
        .getByRole("button", { name: "Settings" })
        .evaluate((el) => getComputedStyle(el).borderRadius),
    )
    expect(fofaControlRadius).toBeLessThan(zwaiControlRadius)
    const panelRadius = await root.evaluate((el) =>
      getComputedStyle(el).getPropertyValue("--radius-lg").trim(),
    )
    // CSS serializes 0.5rem as ".5rem"; compare the length, not the text.
    expect(parseFloat(panelRadius)).toBe(0.5)

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.ui.palette).toBe("fofa")

    await page.reload()
    await expect(page.locator("html")).toHaveAttribute("data-palette", "fofa")
  } finally {
    await request.put("/api/settings", {
      data: { ui: { palette: "zwai" } },
    })
  }
})

test("switches the chrome language and restores English", async ({
  page,
  request,
}) => {
  // Locale is stored in the shared e2e config.yaml. Leave it English or
  // every later spec that looks for "Idle" / "New conversation" dies.
  await page.goto("/")
  try {
    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch language" }).click()
    await page.getByRole("button", { name: "应用菜单" }).click()
    await expect(page.getByRole("menuitem", { name: "切换语言" })).toBeVisible()
    await expect(page.getByTestId("dest-chats")).toHaveAttribute("aria-label", "对话")
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN")
  } finally {
    await request.put("/api/settings", { data: { ui: { locale: "system" } } })
    // The Chinese menu is already open after the assertion. Reload applies
    // the restored setting without clicking through its dismiss overlay.
    await page.reload()
    await page.getByRole("button", { name: "App menu" }).click()
    await expect(page.getByRole("menuitem", { name: "Switch language" })).toBeVisible()
    await expect(page.locator("html")).toHaveAttribute("lang", "en")
  }
})

test("font and conversation width round-trip through settings", async ({
  page,
  request,
}) => {
  await page.goto("/")
  try {
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "General" }).click()
    await dialog.getByRole("combobox", { name: "UI font", exact: true }).click()
    await page.getByRole("option", { name: "Serif" }).click()
    await dialog.getByRole("combobox", { name: "Content font size" }).click()
    await page.getByRole("option", { name: "Large" }).click()
    await dialog.getByRole("combobox", { name: "Conversation width" }).click()
    await page.getByRole("option", { name: "Wide" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const root = page.locator("html")
    await expect(root).toHaveAttribute("data-font", "serif")
    await expect(root).toHaveAttribute("data-font-size", "large")
    await expect(root).toHaveAttribute("data-content-width", "full")
    await expect(root).toHaveCSS("font-size", "16px")

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.ui.font).toBe("serif")
    expect(saved.settings.ui.font_size).toBe("large")
    expect(saved.settings.ui.content_width).toBe("full")

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "General" }).click()
    await expect(
      page.getByRole("dialog").getByRole("combobox", { name: "UI font", exact: true }),
    ).toContainText("Serif")
    await expect(
      page.getByRole("dialog").getByRole("combobox", { name: "Conversation width" }),
    ).toContainText("Wide")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", {
      data: {
        ui: {
          locale: "system",
          font: "system",
          font_size: "medium",
          content_width: "comfortable",
        },
      },
    })
  }
})

test("directory rows track UI size, not conversation size", async ({
  page,
  request,
}) => {
  await page.goto("/")
  try {
    await request.put("/api/settings", {
      data: { ui: { ui_font_size: "medium", font_size: "large" } },
    })
    await page.reload()
    const root = page.locator("html")
    await expect(root).toHaveAttribute("data-ui-font-size", "medium")
    await expect(root).toHaveAttribute("data-font-size", "large")
    const afterContent = await page.evaluate(() => ({
      chrome: getComputedStyle(document.documentElement)
        .getPropertyValue("--chrome-font-size")
        .trim(),
      content: getComputedStyle(document.documentElement)
        .getPropertyValue("--ui-font-size")
        .trim(),
      row: getComputedStyle(document.documentElement)
        .getPropertyValue("--sidebar-row-height")
        .trim(),
    }))
    expect(afterContent.chrome).toBe("13px")
    expect(afterContent.content).toBe("16px")
    expect(afterContent.row).toBe("28px")

    await request.put("/api/settings", {
      data: { ui: { ui_font_size: "large", font_size: "medium" } },
    })
    await page.reload()
    await expect(root).toHaveAttribute("data-ui-font-size", "large")
    await expect(root).toHaveAttribute("data-font-size", "medium")
    const afterUI = await page.evaluate(() => ({
      chrome: getComputedStyle(document.documentElement)
        .getPropertyValue("--chrome-font-size")
        .trim(),
      content: getComputedStyle(document.documentElement)
        .getPropertyValue("--ui-font-size")
        .trim(),
      row: getComputedStyle(document.documentElement)
        .getPropertyValue("--sidebar-row-height")
        .trim(),
    }))
    expect(afterUI.chrome).toBe("16px")
    expect(afterUI.content).toBe("13px")
    expect(afterUI.row).toBe("34px")
  } finally {
    await request.put("/api/settings", {
      data: {
        ui: {
          locale: "system",
          font: "system",
          ui_font_size: "medium",
          font_size: "ui",
          content_width: "comfortable",
        },
      },
    })
  }
})

test("each optional AI feature owns a right-side drawer and its saved setting", async ({ page, request }) => {
  const { settings: original } = await (await request.get("/api/settings")).json()
  const panels = [
    ["Name conversations automatically", "auto_title"],
    ["Suggest a next step", "composer_predictions"],
    ["Automatic graph recording", "graph_auto_record"],
    ["Translation", "selection_translation"],
    ["Voice input", "input"],
    ["Read aloud", "output"],
  ] as const

  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    await page.getByRole("tab", { name: "Models", exact: true }).click()
    await expect(page.getByRole("heading", { name: "Optional AI features" })).toBeVisible()
    for (const [label, field] of panels) {
      const entry = page.getByRole("button", { name: `Configure ${label}`, exact: true })
      await expect(entry).toHaveCount(1)
      await entry.click()
      const drawer = page.getByRole("dialog", { name: label, exact: true })
      const toggle = drawer.getByRole("switch", { name: label, exact: true })
      await expect(toggle).toBeVisible()
      await expect.poll(async () => {
        const bounds = await drawer.boundingBox()
        return bounds ? Math.abs(bounds.x + bounds.width - page.viewportSize()!.width) : Infinity
      }).toBeLessThan(2)
      if (field === "auto_title") {
        await expect(drawer.getByText("Uses the default conversation model from local settings.")).toBeVisible()
        await expect(drawer.getByRole("combobox")).toHaveCount(0)
      }
      const checked = !(await toggle.isChecked())
      await toggle.setChecked(checked)
      await expect.poll(async () => {
        const { settings } = await (await request.get("/api/settings")).json()
        return field === "input" || field === "output" ? settings.speech[field].enabled : settings.swarm[field]
      }).toBe(checked)
      await page.keyboard.press("Escape")
      await expect(drawer).toBeHidden()
      await expect(entry).toBeFocused()
      await entry.click()
      await expect(toggle).toHaveAttribute("aria-checked", String(checked))
      await drawer.getByRole("button", { name: "Close", exact: true }).click()
    }
  } finally {
    await request.put("/api/settings", { data: { swarm: original.swarm, speech: original.speech } })
  }
})
