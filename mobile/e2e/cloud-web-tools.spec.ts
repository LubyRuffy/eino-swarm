import { expect, test, type Page, type Route } from "@playwright/test"

const origin = "https://cloud.invalid"
const modelOrigin = "https://personal.invalid"
const token = "agm_" + "a".repeat(64)
const user = { id: "owner", username: "工具测试账号", enabled: true, approval_status: "approved" }
const device = { id: "phone", expires_at: "2099-01-01T00:00:00Z" }
const cors = { "access-control-allow-origin": "*", "access-control-allow-headers": "authorization, content-type, accept-encoding", "access-control-allow-methods": "GET,POST,OPTIONS" }

async function fixture(page: Page, api: "chat" | "responses", denied = false, fetchTimeout?: Promise<void>) {
  await page.addInitScript(({ origin, token, user, device, api, modelOrigin }) => {
    localStorage.setItem("zwai.phone.gateway.origin", origin)
    localStorage.setItem("zwai.phone.gateway.session", JSON.stringify({ origin, token, user, device }))
    localStorage.setItem("zwai.phone.providers", JSON.stringify([{ id: "personal", label: "手机自配工具模型", baseURL: modelOrigin + "/v1", apiKey: "personal-secret", api, model: "tool-model", catalog: ["tool-model"], timeoutSeconds: 30 }]))
  }, { origin, token, user, device, api, modelOrigin })
  const calls: { path: string; auth: string; body: Record<string, unknown> }[] = []
  let models = 0
  const handleModel = async (route: Route, expectedKey: string) => {
    const request = route.request()
    if (request.method() === "OPTIONS") { await route.fulfill({ headers: cors }); return }
    const body = request.postDataJSON() as Record<string, unknown>
    calls.push({ path: new URL(request.url()).pathname, auth: request.headers().authorization || "", body })
    expect(request.headers().authorization).toBe("Bearer " + expectedKey)
    expect(JSON.stringify(body)).not.toContain(token)
    models++
    if (!body.tools) {
      const events = api === "chat" ? [{ choices: [{ delta: { content: "普通模型对话" }, finish_reason: "stop" }] }, "[DONE]"] : [{ type: "response.output_text.delta", delta: "普通模型对话" }, { type: "response.completed", response: { id: "plain", status: "completed", output: [] } }]
      await route.fulfill({ headers: cors, contentType: "text/event-stream", body: events.map((event) => "data: " + (typeof event === "string" ? event : JSON.stringify(event)) + "\n\n").join("") }); return
    }
    expect((body.tools as { type: string }[]).every((tool) => tool.type === "function")).toBe(true)
    const name = models === 1 ? "web_search" : "web_fetch"
    const args = JSON.stringify(models === 1 ? { query: "current information" } : { url: "https://public.invalid/page", render: !fetchTimeout })
    let events: unknown[]
    if (models > 2) events = api === "chat" ? [{ choices: [{ delta: { content: "依据云端证据完成" }, finish_reason: "stop" }] }, "[DONE]"] : [{ type: "response.output_text.delta", delta: "依据云端证据完成" }, { type: "response.completed", response: { id: "response-3", status: "completed", output: [] } }]
    else if (api === "chat") events = [{ choices: [{ delta: { tool_calls: [{ index: 0, id: "call-" + models, type: "function", function: { name, arguments: args.slice(0, 8) } }] } }] }, { choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: args.slice(8) } }] }, finish_reason: "tool_calls" }] }, "[DONE]"]
    else events = [{ type: "response.output_item.added", output_index: 0, item: { type: "function_call", id: "item-" + models, call_id: "call-" + models, name, arguments: "" } }, { type: "response.function_call_arguments.delta", item_id: "item-" + models, delta: args }, { type: "response.completed", response: { id: "response-" + models, status: "completed", output: [{ type: "function_call", id: "item-" + models, call_id: "call-" + models, name, arguments: args }] } }]
    if (fetchTimeout && models === 2) events.unshift({ choices: [{ delta: { content: "已找到搜索结果。" } }] })
    if (models > 1 && api === "responses") expect(body.previous_response_id).toBe("response-" + (models - 1))
    await route.fulfill({ headers: cors, contentType: "text/event-stream", body: events.map((event) => "data: " + (typeof event === "string" ? event : JSON.stringify(event)) + "\n\n").join("") })
  }
  await page.route(origin + "/**", async (route) => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (request.method() === "OPTIONS") { await route.fulfill({ headers: cors }); return }
    const body = request.postData() ? request.postDataJSON() as Record<string, unknown> : {}
    if (path === "/v1/chat/completions" || path === "/v1/responses") { await handleModel(route, token); return }
    calls.push({ path, auth: request.headers().authorization || "", body })
    let data: unknown = {}
    if (path === "/client/api/v1/auth/me") data = { user, device }
    else if (path === "/client/api/v1/bootstrap") data = { user, device, models: { source_id: "gateway:owner", source_kind: "gateway", label: "测试服务器", execution: "phone", api_base_url: origin + "/v1", catalog_url: origin + "/v1/models", api_access_enabled: true }, pairlink: { enabled: true, hub_url: origin, redeem_url: origin + "/pairlink/v1/pairings/redeem", websocket_url: origin.replace("https:", "wss:") + "/pairlink/v1/ws", info_url: origin + "/pairlink/v1/info" }, heartbeat_interval: 30, presence_ttl: 90 }
    else if (path === "/client/api/v1/link-hosts") data = []
    else if (path === "/v1/models") { await route.fulfill({ headers: cors, contentType: "application/json", body: JSON.stringify({ data: [{ id: "server-model" }] }) }); return }
    else if (path === "/v1/web/search" || path === "/v1/web/fetch") {
      expect(request.headers().authorization).toBe("Bearer " + token)
      if (denied) { await route.fulfill({ status: 403, headers: cors, contentType: "application/json", body: JSON.stringify({ error: { code: "permission_denied" } }) }); return }
      if (path.endsWith("/search")) data = { provider: "fixture", query: body.query, results: [{ title: "云端搜索证据", url: "https://public.invalid/page", snippet: "云端摘要", position: 1 }] }
      else {
        if (fetchTimeout) {
          expect(body).toEqual({ url: "https://public.invalid/page", formats: ["markdown"] })
          await fetchTimeout
          await route.fulfill({ status: 504, headers: cors, contentType: "application/json", body: JSON.stringify({ success: false, error: { code: "fetch_timeout", message: "private fixture diagnostic" } }) })
          return
        }
        expect(body).toMatchObject({ url: "https://public.invalid/page", formats: ["markdown"], actions: [{ type: "wait", milliseconds: 1 }] })
        data = { markdown: "# 云端网页正文", url: "https://public.invalid/page", source: "camofox" }
      }
      await route.fulfill({ headers: cors, contentType: "application/json", body: JSON.stringify({ success: true, data }) }); return
    } else if (!["/client/api/v1/heartbeat", "/client/api/v1/auth/logout"].includes(path)) { await route.abort(); return }
    await route.fulfill({ headers: cors, contentType: "application/json", body: JSON.stringify({ data, meta: { total: 0 } }) })
  })
  await page.route(modelOrigin + "/**", (route) => handleModel(route, "personal-secret"))
  return { calls, models: () => models }
}

async function chooseModel(page: Page, source: "personal" | "gateway" = "personal") {
  await page.goto("/")
  await expect(page.getByRole("tab", { name: "对话" })).toHaveAttribute("aria-selected", "true")
  await page.getByRole("button", { name: "模型", exact: true }).click()
  const group = page.getByRole("heading", { name: source === "personal" ? "手机自配工具模型" : /测试服务器/ }).locator("..")
  await group.getByRole("radio", { name: source === "personal" ? "tool-model" : "server-model", exact: true }).click()
}

async function expectNoWebSettingsBanner(page: Page) {
  await expect(page.getByTestId("direct-web-status")).toHaveCount(0)
  await expect(page.getByTestId("pc-web-status")).toHaveCount(0)
  await expect(page.getByRole("button", { name: "联网设置", exact: true })).toHaveCount(0)
}

async function openUserCenterFromMenu(page: Page) {
  await page.getByRole("button", { name: "菜单" }).click()
  await page.getByRole("menuitem", { name: "用户中心" }).click()
  return page.getByRole("dialog", { name: "用户中心", exact: true })
}

for (const api of ["chat", "responses"] as const) {
  for (const source of (api === "chat" ? ["personal", "gateway"] : ["personal"]) as ("personal" | "gateway")[]) test(`${api}: signed-in phone executes cloud tools with the ${source} model`, async ({ page }, testInfo) => {
    const seen = await fixture(page, api)
    await chooseModel(page, source)
    await expectNoWebSettingsBanner(page)
    await page.getByLabel("消息").fill("Use web evidence")
    await page.getByRole("button", { name: "发送", exact: true }).click()
    await expect(page.getByText("依据云端证据完成", { exact: true })).toBeVisible()
    await expectNoWebSettingsBanner(page)
    expect(seen.models()).toBe(3)
    expect(seen.calls.filter((call) => call.path === "/v1/web/search")).toHaveLength(1)
    expect(seen.calls.filter((call) => call.path === "/v1/web/fetch")).toHaveLength(1)
    await page.getByTestId("work-fold").click()
    await expect(page.getByText("web_search", { exact: true })).toBeVisible()
    await expect(page.getByText("web_fetch", { exact: true })).toBeVisible()
    await page.getByRole("button", { name: /web_search/ }).click()
    await expect(page.getByText(/云端摘要/)).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath("cloud-web-tools-" + api + ".png") })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test("a page timeout preserves streamed text and the model continues after one retry", async ({ page }, testInfo) => {
  let completeFetch!: () => void
  const fetchTimeout = new Promise<void>((resolve) => { completeFetch = resolve })
  const seen = await fixture(page, "chat", false, fetchTimeout)
  await chooseModel(page, "gateway")
  await page.getByLabel("消息").fill("Use web evidence")
  await page.getByRole("button", { name: "发送", exact: true }).click()
  try {
    await expect.poll(() => seen.calls.filter((call) => call.path === "/v1/web/fetch").length).toBe(1)
    await expect(page.getByText("已找到搜索结果。", { exact: true })).toBeVisible()
  } finally {
    completeFetch()
  }

  await expect(page.getByText("已找到搜索结果。依据云端证据完成", { exact: true })).toBeVisible()
  await expect(page.getByRole("alert")).toHaveCount(0)
  expect(seen.models()).toBe(3)
  expect(seen.calls.filter((call) => call.path === "/v1/web/search")).toHaveLength(1)
  expect(seen.calls.filter((call) => call.path === "/v1/web/fetch")).toHaveLength(2)
  const continuation = seen.calls.filter((call) => call.path === "/v1/chat/completions").at(-1)?.body
  expect(JSON.stringify(continuation)).toContain("fetch_timeout")
  expect(JSON.stringify(continuation)).not.toContain("private fixture diagnostic")
  const tools = page.getByTestId("work-fold")
  await expect(tools).toHaveClass(/text-destructive/)
  await tools.click()
  await expect(page.getByText("web_fetch", { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("cloud-web-fetch-timeout.png") })
})

test("permission denial is visible; disabling phone Web persists without overriding PC selection", async ({ page }, testInfo) => {
  const seen = await fixture(page, "chat", true)
  await chooseModel(page)
  await page.getByLabel("消息").fill("Use web evidence")
  await page.getByRole("button", { name: "发送", exact: true }).click()
  const denial = page.getByRole("alert")
  await expect(denial).toContainText("云端联网工具权限当前不可用")
  expect(seen.models()).toBe(1)
  await expectNoWebSettingsBanner(page)
  await page.getByRole("button", { name: "用户中心", exact: true }).click()
  const center = page.getByRole("dialog", { name: "用户中心", exact: true })
  await expect(center.getByTestId("direct-web-status")).toContainText("云端服务器 · 使用时校验权限")
  await expect(page.getByLabel("手机直聊联网工具", { exact: true })).toHaveValue("auto")
  await page.getByLabel("手机直聊联网工具", { exact: true }).selectOption("off")
  await expect(center.getByTestId("direct-web-status")).toContainText("已关闭 · 普通模型对话")
  await expect(page.getByLabel("电脑会话联网工具", { exact: true })).toHaveValue("inherit")
  await page.getByLabel("电脑会话联网工具", { exact: true }).selectOption("local")
  await expect(page.getByLabel("手机直聊联网工具", { exact: true })).toHaveValue("off")
  await expect(center.getByText(/自配模型需要支持工具调用/)).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("cloud-web-independent-settings.png") })
  await center.getByRole("button", { name: "关闭", exact: true }).click()
  await expect(denial).toContainText("云端联网工具权限当前不可用")
  await expectNoWebSettingsBanner(page)
  await page.getByLabel("消息").fill("Ordinary conversation")
  await page.getByRole("button", { name: "发送", exact: true }).click()
  await expect(page.getByText("普通模型对话", { exact: true })).toBeVisible()
  expect(seen.calls.filter((call) => call.path === "/v1/web/search")).toHaveLength(1)
  expect(await page.evaluate(() => Object.entries(localStorage).filter(([key]) => key.startsWith("zwai.phone.directWebExecution:")).map(([, value]) => value))).toEqual(["off"])
})

test("a signed-in phone inherits an anonymous PC and keeps direct-chat tools independent", async ({ page }, info) => {
  await fixture(page, "chat")
  await page.goto("/?mock=1&tick=0")
  await expect(page.getByTestId("transcript")).toBeVisible()
  await expectNoWebSettingsBanner(page)
  await page.getByRole("button", { name: "用户中心", exact: true }).click()
  const center = page.getByRole("dialog", { name: "用户中心", exact: true })
  await expect(center.getByTestId("pc-web-status")).toContainText("跟随电脑（最近刷新）")
  await expect(center.getByTestId("pc-web-status")).toContainText("在这台电脑执行")
  await expect(page.getByLabel("手机直聊联网工具", { exact: true })).toHaveValue("auto")
  await expect(page.getByLabel("电脑会话联网工具", { exact: true })).toHaveValue("inherit")
  await page.getByLabel("手机直聊联网工具", { exact: true }).selectOption("off")
  await expect(page.getByLabel("电脑会话联网工具", { exact: true })).toHaveValue("inherit")
  await expect(center.getByTestId("pc-web-status")).toContainText("在这台电脑执行")
  await center.getByRole("button", { name: "关闭", exact: true }).click()
  await expect(page.getByTestId("transcript")).toBeVisible()
  await expectNoWebSettingsBanner(page)
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await expect(page.getByRole("button", { name: "菜单" })).toBeVisible()
  await expectNoWebSettingsBanner(page)
  await openUserCenterFromMenu(page)
  await expect(center.getByTestId("pc-web-status")).toContainText("在这台电脑执行")
  await page.getByLabel("电脑会话联网工具", { exact: true }).selectOption("cloud")
  await expect(center.getByTestId("pc-web-status")).toContainText("请在这台电脑登录")
  await center.getByRole("button", { name: "关闭", exact: true }).click()
  await page.getByTestId("new-chat-top").click()
  await expect(page.getByRole("heading", { name: "新建对话", exact: true })).toBeVisible()
  await expectNoWebSettingsBanner(page)
  await page.screenshot({ path: info.outputPath("phone-account-anonymous-PC.png") })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test("an anonymous explicit cloud selection refuses before the personal model request", async ({ page }, info) => {
  const seen = await fixture(page, "chat")
  await page.addInitScript(() => {
    localStorage.removeItem("zwai.phone.gateway.session")
    localStorage.setItem("zwai.phone.directWebExecution:anonymous", "cloud")
  })
  await chooseModel(page)
  await expectNoWebSettingsBanner(page)
  const center = await openUserCenterFromMenu(page)
  await expect(center.getByTestId("direct-web-status")).toContainText("请登录或关闭联网工具")
  await center.getByRole("button", { name: "关闭", exact: true }).click()
  await page.getByLabel("消息").fill("Use web evidence")
  await page.getByRole("button", { name: "发送", exact: true }).click()
  await expect(page.getByRole("alert")).toContainText("请登录")
  expect(seen.models()).toBe(0)
  expect(seen.calls.filter((call) => call.path.startsWith("/v1/web/"))).toHaveLength(0)
  await page.screenshot({ path: info.outputPath("cloud-needs-login-before-model.png") })
})
