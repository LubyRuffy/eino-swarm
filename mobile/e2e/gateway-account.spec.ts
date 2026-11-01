import { expect, test, type Page } from "@playwright/test"
import { startAccountFixture } from "./account-fixture/browser-fixture"

const origin = "https://ai.fofa.info:2440"
const token = "agm_" + "a".repeat(64)
const user = { id: "owner", username: "Test User", enabled: true, approval_status: "approved" }
const device = { id: "mobile", expires_at: "2099-01-01T00:00:00Z" }
const bootstrap = { user, device, models: { source_id: "gateway:owner", source_kind: "gateway", label: "测试网关", execution: "phone", api_base_url: origin + "/v1", catalog_url: origin + "/v1/models", api_access_enabled: true }, pairlink: { enabled: true, hub_url: origin, redeem_url: origin + "/pairlink/v1/pairings/redeem", info_url: origin + "/pairlink/v1/info", websocket_url: origin.replace("https:", "wss:") + "/pairlink/v1/ws" }, heartbeat_interval: 30, presence_ttl: 90 }

async function approved(page: Page, withPersonal = false) {
  await page.addInitScript(({ origin, token, user, device, withPersonal }) => {
    localStorage.setItem("zwai.phone.gateway.session", JSON.stringify({ origin, token, user, device }))
    if (withPersonal) localStorage.setItem("zwai.phone.providers", JSON.stringify([{ id: "p-private", label: "手机自配", baseURL: "https://personal.invalid/v1", apiKey: "private-key", api: "chat", model: "shared-model", catalog: ["shared-model"], timeoutSeconds: 300 }]))
  }, { origin, token, user, device, withPersonal })
}

async function fixture(page: Page, options: { apiEnabled?: boolean; hosts?: unknown[]; review?: string; denyBootstrap?: boolean; denyConnect?: boolean } = {}) {
  const calls: { path: string; authorization: string; body: unknown }[] = []
  let connectionRefused = false
  await page.context().route(origin + "/**", async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const body = request.postData() ? request.postDataJSON() : undefined
    calls.push({ path, authorization: request.headers().authorization || "", body })
    let data: unknown = {}
    if (path === "/client/api/v1/auth/mobile/start") data = { device_code: "d".repeat(43), user_code: "1234-5678-abcd-ef01", verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=1234-5678-abcd-ef01", expires_at: new Date(Date.now() + 600_000).toISOString(), interval: 5 }
    else if (path === "/client/api/v1/auth/mobile/poll") data = { status: options.review || "account_pending" }
    else if (path === "/client/api/v1/auth/me") data = { user: connectionRefused ? { ...user, approval_status: "rejected" } : user, device }
    else if (path === "/client/api/v1/bootstrap" && options.denyBootstrap) { await route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: { code: "permission_denied" } }) }); return }
    else if (path === "/client/api/v1/bootstrap") data = { ...bootstrap, models: { ...bootstrap.models, api_access_enabled: options.apiEnabled !== false } }
    else if (path === "/client/api/v1/link-hosts") data = options.hosts || []
    else if (path === "/client/api/v1/link-hosts/pc-online/connect" && options.denyConnect) { connectionRefused = true; await route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: { code: "permission_denied" } }) }); return }
    else if (path === "/client/api/v1/link-hosts/pc-online/connect") data = { binding_id: "binding", ticket: "b".repeat(64), host_pub: "D".repeat(42) + "A", session_id: "c".repeat(32) }
    else if (path === "/v1/models") data = [{ id: "shared-model" }]
    else if (path === "/v1/chat/completions") {
      expect(request.headers().authorization).toBe("Bearer " + token)
      expect(body.model).toBe("shared-model")
      await route.fulfill({ contentType: "text/event-stream", body: 'data: {"choices":[{"delta":{"content":"SERVER ANSWER"}}]}\n\ndata: [DONE]\n\n' })
      return
    } else if (path === "/client/api/v1/auth/sso/nosec/start") {
      await route.fulfill({ contentType: "text/html", body: "<p>Private test authorization fixture</p>" }); return
    } else if (!["/client/api/v1/heartbeat", "/client/api/v1/auth/logout"].includes(path)) {
      await route.abort(); return
    }
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ data, meta: { total: options.hosts?.length || 0 } }) })
  })
  return calls
}

async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

test("account overlays keep the title and close control inside the safe area", async ({ page, browserName }, testInfo) => {
  test.skip(browserName !== "chromium", "Chromium exposes CSS safe-area env overrides through CDP; iOS also has a native layout gate")
  const insets = { top: 59, right: 16, bottom: 34, left: 10 }
  const cdp = await page.context().newCDPSession(page)
  await cdp.send("Emulation.setSafeAreaInsetsOverride", { insets })
  await fixture(page)
  await page.goto("/")
  expect(await page.locator("body").evaluate((body) => Number.parseFloat(getComputedStyle(body).paddingTop))).toBe(insets.top)

  const checkOverlay = async (name: string) => {
    const dialog = page.getByRole("dialog", { name: "用户中心", exact: true })
    const title = dialog.getByRole("heading", { name: "用户中心", exact: true })
    const close = dialog.getByRole("button", { name: "关闭", exact: true })
    await expect(dialog).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(name + ".png") })
    for (const element of [title, close]) {
      const bounds = await element.boundingBox()
      expect(bounds).not.toBeNull()
      expect(bounds!.y).toBeGreaterThanOrEqual(insets.top)
      expect(bounds!.y).toBeLessThan(insets.top + 48)
      expect(bounds!.x).toBeGreaterThanOrEqual(insets.left)
      expect(bounds!.x + bounds!.width).toBeLessThanOrEqual((await page.evaluate(() => innerWidth)) - insets.right)
    }
    expect(await close.evaluate((button) => {
      const bounds = button.getBoundingClientRect()
      return button.contains(document.elementFromPoint(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2))
    })).toBe(true)
    await noOverflow(page)
    await close.click()
    await expect(dialog).toHaveCount(0)
  }

  await page.getByRole("button", { name: "使用 NOSEC 登录" }).click()
  await checkOverlay("signed-out-safe-area")
  await expect(page.getByRole("button", { name: "扫描二维码" })).toBeVisible()

  await approved(page)
  await page.goto("/")
  await expect(page.getByRole("tab", { name: "对话" })).toHaveAttribute("aria-selected", "true")
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "用户中心" }).click()
  await checkOverlay("approved-safe-area")
  await expect(page.getByRole("tab", { name: "对话" })).toBeVisible()
})

test("default NOSEC sign-in requires no server configuration and review hides all other resources", async ({ page }, testInfo) => {
  const calls = await fixture(page)
  await page.goto("/")
  await page.getByRole("button", { name: "使用 NOSEC 登录" }).click()
  await expect(page.getByLabel("服务器地址")).toHaveCount(0)
  await page.getByRole("button", { name: "使用 NOSEC 登录" }).click()
  await expect(page.getByText("等待管理员审核")).toBeVisible({ timeout: 12_000 })
  await expect(page.getByRole("button", { name: "扫描二维码" })).toHaveCount(0)
  await expect(page.getByLabel("消息")).toHaveCount(0)
  await expect(page.getByRole("heading", { name: "我的电脑" })).toHaveCount(0)
  expect(calls.filter((c) => !["/client/api/v1/auth/mobile/start", "/client/api/v1/auth/mobile/poll", "/client/api/v1/auth/sso/nosec/start"].includes(c.path))).toEqual([])
  const start = calls.find((c) => c.path.endsWith("/start") && c.path.includes("/mobile/"))!
  expect(start.body).toMatchObject({ candidate_token: expect.stringMatching(/^agm_[a-f0-9]{64}$/), code_challenge: expect.stringMatching(/^[A-Za-z0-9_-]{43}$/) })
  await noOverflow(page)
  await page.screenshot({ path: testInfo.outputPath("account-review.png") })
})

test("server and phone same-named models route by selected source and logout preserves private settings", async ({ page }, testInfo) => {
  await approved(page, true)
  const calls = await fixture(page)
  let privateCalls = 0
  await page.route("https://personal.invalid/**", async (route) => {
    privateCalls++
    expect(route.request().headers().authorization).toBe("Bearer private-key")
    await route.fulfill({ contentType: "text/event-stream", body: 'data: {"choices":[{"delta":{"content":"PRIVATE ANSWER"}}]}\n\ndata: [DONE]\n\n' })
  })
  await page.goto("/")
  await expect(page.getByRole("tab", { name: "对话" })).toHaveAttribute("aria-selected", "true")
  await page.getByRole("button", { name: "模型", exact: true }).click()
  await expect(page.getByRole("heading", { name: "服务器 · 测试网关" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "手机自配" })).toBeVisible()
  await expect(page.getByRole("radio", { name: "shared-model" })).toHaveCount(2)
  await page.screenshot({ path: testInfo.outputPath("model-sources.png") })
  const serverGroup = page.getByRole("heading", { name: "服务器 · 测试网关" }).locator("..")
  await serverGroup.getByRole("radio", { name: "shared-model" }).click()
  await page.getByLabel("消息").fill("route through server")
  await page.getByRole("button", { name: "发送", exact: true }).click()
  await expect(page.getByText("SERVER ANSWER", { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("server-chat.png") })
  expect(privateCalls).toBe(0)
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "用户中心" }).click()
  await noOverflow(page)
  await page.screenshot({ path: testInfo.outputPath("account-devices.png") })
  await page.getByRole("button", { name: "退出登录" }).click()
  await page.getByRole("button", { name: "关闭", exact: true }).click()
  expect(await page.evaluate(() => localStorage.getItem("zwai.phone.providers"))).toContain("private-key")
  expect(await page.evaluate(() => localStorage.getItem("zwai.phone.providers"))).not.toContain(token)
  await page.getByRole("button", { name: "模型", exact: true }).click()
  await expect(page.getByRole("radio", { name: "shared-model" })).toHaveCount(1)
  await page.getByRole("radio", { name: "shared-model" }).click()
  await page.getByLabel("消息").fill("route through personal source")
  await page.getByRole("button", { name: "发送", exact: true }).click()
  await expect(page.getByText("PRIVATE ANSWER", { exact: true })).toBeVisible()
  expect(privateCalls).toBe(1)
  expect(calls.filter((c) => c.path === "/v1/chat/completions")).toHaveLength(1)
})

test("device-only account can select an online PC without QR or API permission", async ({ page }, testInfo) => {
  await approved(page)
  const calls = await fixture(page, { apiEnabled: false, hosts: [
    { id: "pc-online", name: "办公电脑", hostname: "work", version: "1", online: true, enabled: true },
    { id: "pc-offline", name: "家里电脑", hostname: "home", version: "1", online: false, enabled: true },
  ] })
  await page.goto("/?mock=1&tick=0")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "用户中心" }).click()
  await expect(page.getByRole("heading", { name: "我的电脑" })).toBeVisible()
  await expect(page.getByText("API 权限当前不可用，请联系管理员。")).toBeVisible()
  await expect(page.getByRole("button", { name: "连接 家里电脑" })).toBeDisabled()
  await noOverflow(page)
  await page.screenshot({ path: testInfo.outputPath("account-pc-picker.png") })
  await page.getByRole("button", { name: "连接 办公电脑" }).click()
  await expect(page.getByRole("tablist")).toBeVisible()
  expect(calls.filter((c) => c.path.endsWith("/connect"))).toHaveLength(1)
  expect(calls.find((c) => c.path.endsWith("/connect"))).toMatchObject({ authorization: "Bearer " + token, body: { device_pub: expect.any(String) } })
  expect(calls.filter((c) => c.path.includes("pairings") || c.path === "/v1/models")).toEqual([])
  const links = await page.evaluate(() => JSON.parse(localStorage.getItem("zwai.remote.links") || "[]"))
  expect(links.filter((link: { gatewayHostID?: string }) => Boolean(link.gatewayHostID))).toHaveLength(1)
  expect(links.find((link: { gatewayHostID?: string }) => Boolean(link.gatewayHostID))).toMatchObject({ gatewayHostID: "pc-online", gatewayOwnerID: "owner", gatewayOrigin: origin })
})

test("same-account login merges a saved anonymous PC and preserves the selected independent Hub", async ({ page }, testInfo) => {
  await approved(page)
  const hostPub = "D".repeat(42) + "A"
  await page.addInitScript(({ origin, hostPub }) => {
    localStorage.setItem("zwai.remote.links", JSON.stringify([
      { hubURL: origin + "/", hostPub, fingerprint: "saved-pc", ticket: "anonymous", sessionID: "a".repeat(32), label: "原电脑" },
      { hubURL: "https://independent.invalid", hostPub, fingerprint: "other-hub", ticket: "other", sessionID: "b".repeat(32), label: "独立电脑" },
    ]))
    localStorage.setItem("zwai.remote.active", "other-hub")
    localStorage.setItem("zwai.remote.lastThread.saved-pc", "last-thread")
  }, { origin, hostPub })
  // Exercise real storage/UI with synthetic control responses; never open an
  // external Pairlink socket from this browser regression.
  await page.routeWebSocket("wss://**", (socket) => socket.close())
  const calls = await fixture(page, { hosts: [{ id: "pc-online", name: "办公电脑", hostname: "work", version: "1", online: true, enabled: true, owner_user_id: user.id, host_pub: hostPub }] })
  await page.goto("/")
  await expect.poll(() => calls.filter((call) => call.path.endsWith("/connect")).length).toBe(1)
  await expect.poll(async () => page.evaluate(() => JSON.parse(localStorage.getItem("zwai.remote.links") || "[]")[0].gatewayOwnerID)).toBe(user.id)
  const state = await page.evaluate(() => ({ links: JSON.parse(localStorage.getItem("zwai.remote.links") || "[]"), active: localStorage.getItem("zwai.remote.active"), last: localStorage.getItem("zwai.remote.lastThread.saved-pc") }))
  expect(state).toMatchObject({ active: "other-hub", last: "last-thread", links: [
    { fingerprint: "saved-pc", gatewayOwnerID: user.id, gatewayHostID: "pc-online", label: "原电脑", ticket: "b".repeat(64) },
    { fingerprint: "other-hub", ticket: "other" },
  ] })
  expect(state.links).toHaveLength(2)
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "用户中心" }).click()
  await page.getByRole("button", { name: "刷新账号" }).click()
  expect(calls.filter((call) => call.path.endsWith("/connect"))).toHaveLength(1)
  expect(calls.some((call) => call.path.includes("pairings"))).toBe(false)
  await page.screenshot({ path: testInfo.outputPath("anonymous-account-merged.png") })
  await page.getByRole("button", { name: "退出登录" }).click()
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem("zwai.remote.links") || "[]").map((link: { fingerprint: string }) => link.fingerprint))).toEqual(["other-hub"])
})

test("real anonymous QR becomes one account connection and the replacement ticket reaches PC RPC", async ({ page }, testInfo) => {
  test.setTimeout(150_000)
  const real = await startAccountFixture(testInfo)
  const state = async () => (await (await page.request.get(real.origin + "/fixture/state")).json()).data as {
    counts: Record<string, number>; active_bindings: number; retired_bindings: number; host_online: boolean; mobile_online: boolean
  }
  const sockets: { closed: boolean; received: number }[] = []
  page.on("websocket", (socket) => {
    if (!socket.url().startsWith(real.origin.replace("http:", "ws:") + "/pairlink/")) return
    const observed = { closed: false, received: 0 }
    sockets.push(observed)
    socket.on("close", () => { observed.closed = true })
    socket.on("framereceived", () => { observed.received++ })
  })
  try {
    await page.request.get(real.origin + "/fixture/control?review=approved&pc=hide&api=off")
    const offer = (await (await page.request.get(real.origin + "/fixture/anonymous-offer?origin=primary")).json()).data.uri as string
    await page.addInitScript((gateway) => { localStorage.setItem("zwai.phone.gateway.origin", gateway) }, real.origin)
    await page.goto("/")
    await page.getByLabel("配对 URI").fill(offer)
    await page.getByRole("button", { name: "粘贴并绑定", exact: true }).click()
    await expect(page.getByRole("button", { name: /Native PC conversation/ })).toBeVisible()
    const anonymous = await page.evaluate(() => JSON.parse(localStorage.getItem("zwai.remote.links") || "[]"))
    expect(anonymous).toHaveLength(1)
    expect(anonymous[0].gatewayOwnerID).toBeUndefined()
    expect((await state()).active_bindings).toBe(1)
    expect((await state()).counts.connect || 0).toBe(0)
    expect(sockets.filter((socket) => !socket.closed)).toHaveLength(1)
    await page.screenshot({ path: testInfo.outputPath("real-anonymous-PC-RPC.png"), animations: "disabled" })

    await page.getByRole("button", { name: "菜单", exact: true }).click()
    await page.getByRole("menuitem", { name: "用户中心", exact: true }).click()
    const popup = page.waitForEvent("popup")
    await page.getByRole("button", { name: "使用 NOSEC 登录", exact: true }).click()
    const browser = await popup
    await browser.getByRole("button", { name: "Sign in to test NOSEC", exact: true }).click()
    await browser.getByRole("button", { name: "Authorize test device", exact: true }).click()
    await expect(browser.getByRole("heading", { name: "Device authorized. Return to zwai.", exact: true })).toBeVisible()
    await browser.close()
    await expect(page.getByText("API 权限当前不可用，请联系管理员。", { exact: true })).toBeVisible({ timeout: 15_000 })
    expect((await state()).counts.connect || 0).toBe(0)
    await expect.poll(() => sockets.filter((socket) => !socket.closed).length).toBe(1)

    // The phone logs in first; a later Desktop login becomes visible on refresh.
    // A new durable PC thread proves the replacement transport did a fresh RPC.
    const proofTitle = "Account upgrade RPC proof"
    await page.request.get(real.origin + "/fixture/control?pc=show&new_thread=" + encodeURIComponent(proofTitle))
    await page.getByRole("button", { name: "刷新账号", exact: true }).click()
    await expect.poll(async () => page.evaluate(() => JSON.parse(localStorage.getItem("zwai.remote.links") || "[]")[0]?.gatewayOwnerID)).toBe("native-owner")
    const managed = await page.evaluate(() => ({ links: JSON.parse(localStorage.getItem("zwai.remote.links") || "[]"), active: localStorage.getItem("zwai.remote.active") }))
    expect(managed.links).toHaveLength(1)
    expect(managed.active).toBe(anonymous[0].fingerprint)
    expect(managed.links[0]).toMatchObject({ fingerprint: anonymous[0].fingerprint, hostPub: anonymous[0].hostPub, gatewayHostID: "native-pc", gatewayOwnerID: "native-owner" })
    expect(managed.links[0].ticket).not.toBe(anonymous[0].ticket)
    expect(managed.links[0].sessionID).not.toBe(anonymous[0].sessionID)
    await page.getByRole("button", { name: "关闭", exact: true }).click()
    await expect(page.getByRole("button", { name: new RegExp(proofTitle) })).toBeVisible()
    await page.getByRole("button", { name: new RegExp(proofTitle) }).click()
    await expect(page.getByRole("heading", { name: proofTitle, exact: true })).toBeVisible()
    // Checking temporarily unmounts PhoneApp; approving it may reopen the
    // anonymous transport before the PC logs in. Historical dials are valid;
    // every one must close before the managed route remains the sole live one.
    expect(sockets.length).toBeGreaterThanOrEqual(2)
    await expect.poll(() => sockets.slice(0, -1).every((socket) => socket.closed)).toBe(true)
    expect(sockets.filter((socket) => !socket.closed)).toHaveLength(1)
    expect(sockets.at(-1)!.received).toBeGreaterThan(0)
    const upgraded = await state()
    expect(upgraded).toMatchObject({ active_bindings: 1, retired_bindings: 1, host_online: true, mobile_online: true })
    expect(upgraded.counts.connect).toBe(1)
    expect(upgraded.counts.qr_redeem).toBe(1)
    expect(upgraded.counts.anonymous_authorization_leak || 0).toBe(0)
    await page.screenshot({ path: testInfo.outputPath("real-account-replacement-PC-RPC.png"), animations: "disabled" })

    await page.getByRole("button", { name: "返回", exact: true }).click()
    await page.getByRole("button", { name: "菜单", exact: true }).click()
    await page.getByRole("menuitem", { name: "用户中心", exact: true }).click()
    await page.getByRole("button", { name: "刷新账号", exact: true }).click()
    expect((await state()).counts.connect).toBe(1)
    await page.getByRole("button", { name: "退出登录", exact: true }).click()
    await expect.poll(async () => (await state()).mobile_online).toBe(false)
    // Local sign-out closes the socket before the asynchronous server logout
    // finishes. Its receipt is incremented only after ticket retirement.
    await expect.poll(async () => (await state()).counts.logout || 0).toBe(1)
    expect(await state()).toMatchObject({ active_bindings: 0, retired_bindings: 2, host_online: true })
    expect(await page.evaluate(() => JSON.parse(localStorage.getItem("zwai.remote.links") || "[]"))).toEqual([])
    await page.screenshot({ path: testInfo.outputPath("real-account-logout-disconnect.png"), animations: "disabled" })
    await testInfo.attach("real-account-upgrade-state", { body: JSON.stringify(await state()), contentType: "application/json" })
  } finally { await real.cleanup() }
})


test("a bootstrap denial remains restricted while account review status is unconfirmed", async ({ page }) => {
  await approved(page, true)
  const calls = await fixture(page, { denyBootstrap: true })
  await page.goto("/")
  await expect(page.getByText("账号访问状态待确认，请刷新或重新登录。")).toBeVisible()
  await expect(page.getByRole("heading", { name: "用户中心" })).toBeVisible()
  await expect(page.getByRole("button", { name: "菜单" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "关闭", exact: true })).toHaveCount(0)
  await expect(page.getByLabel("消息")).toHaveCount(0)
  expect(calls.filter((call) => call.path.startsWith("/v1/") || call.path.endsWith("/link-hosts") || call.path.endsWith("/heartbeat"))).toEqual([])
})


test("a refused PC connection rechecks review and hides private models and device controls", async ({ page }) => {
  await approved(page, true)
  const calls = await fixture(page, { apiEnabled: false, denyConnect: true, hosts: [
    { id: "pc-online", name: "办公电脑", hostname: "work", version: "1", online: true, enabled: true },
  ] })
  await page.goto("/")
  await expect(page.getByRole("tab", { name: "对话" })).toHaveAttribute("aria-selected", "true")
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "用户中心" }).click()
  await page.getByRole("button", { name: "连接 办公电脑" }).click()
  await expect(page.getByText("账号审核未通过，请联系管理员。")).toBeVisible()
  await expect(page.getByRole("heading", { name: "用户中心" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "我的电脑" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "菜单" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "关闭", exact: true })).toHaveCount(0)
  await expect(page.getByLabel("消息")).toHaveCount(0)
  expect(calls.filter((call) => call.path.endsWith("/connect"))).toHaveLength(1)
  expect(calls.filter((call) => call.path.endsWith("/auth/me")).length).toBeGreaterThanOrEqual(2)
  expect(await page.evaluate(() => localStorage.getItem("zwai.phone.providers"))).toContain("private-key")
})
