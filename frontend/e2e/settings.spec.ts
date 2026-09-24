import { expect, test } from "@playwright/test"

test("semantic search is off until a model is pinned", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models" }).click()
  await expect(dialog.getByLabel("Semantic search")).not.toBeChecked()
  await expect(dialog.getByLabel("Embedding model")).toHaveCount(0)
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
  await dialog.getByRole("button", { name: "Back to app" }).click()

  expect(crashes).toEqual([])
})

test("settings round-trip through the config file", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText("config.yaml")).toBeVisible()

  await dialog.getByRole("tab", { name: "Swarm" }).click()
  await expect(
    dialog.getByLabel("Name conversations automatically"),
  ).toBeChecked()
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

test("pins a title-generation model when more than one name is listed", async ({
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
      swarm: { ...settings.swarm, title_provider: "", title_model: "" },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Models" }).click()
    await dialog.getByText("Auxiliary models").scrollIntoViewIfNeeded()
    await expect(dialog.getByText("Auxiliary models")).toBeVisible()
    const picker = dialog.getByLabel("Title generation model")
    await expect(picker).toContainText("Automatic")
    await picker.click()
    await page.getByRole("option", { name: "beta" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.swarm.title_model).toBe("beta")
    expect(saved.settings.swarm.title_provider).toBe(provider.id)

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Models" }).click()
    await expect(
      page.getByRole("dialog").getByLabel("Title generation model"),
    ).toContainText("beta")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
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
    await expect(dialog.getByRole("textbox", { name: "Base URL" })).toHaveCount(0)
    await dialog.getByRole("button", { name: `${heading} details` }).click()
    await expect(dialog.getByLabel("Provider")).toBeVisible()
    await expect(dialog.getByLabel("Base URL")).toHaveValue(
      "http://endpoint.invalid/v1",
    )
    await dialog.getByRole("button", { name: "Discover models" }).click()
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
    const discover = dialog.getByRole("button", { name: "Discover models" })
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

  const last = dialog.getByLabel("Goal auto-compact at (%)")
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
test("switches color palette and remembers it", async ({ page, request }) => {
  await page.goto("/")
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
    const primary = await root.evaluate((el) =>
      getComputedStyle(el).getPropertyValue("--primary").trim(),
    )
    expect(primary.startsWith("180")).toBe(true)

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
    await expect(
      page.getByRole("button", { name: "新对话", exact: true }),
    ).toBeVisible()
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
