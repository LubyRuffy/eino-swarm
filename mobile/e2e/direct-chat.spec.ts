import { expect, test } from "@playwright/test"

/** A phone with no PC can still talk to an OpenAI-compatible endpoint.
 *  The route stands in for that endpoint: no key, no network. */
test("connecting a model adds a chat tab and answers on the phone", async ({ page }) => {
  let wire = ""
  await page.route("http://127.0.0.1:9/**", async (route) => {
    const url = route.request().url()
    if (url.endsWith("/models")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ data: [{ id: "unit-model" }] }),
      })
      return
    }
    if (url.endsWith("/responses")) {
      wire = "responses"
      const sent = route.request().postDataJSON() as { reasoning?: { effort?: string } }
      expect(sent.reasoning).toEqual({ effort: "high" })
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          output: [{ type: "message", content: [{ type: "output_text", text: "pong" }] }],
        }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: 'data: {"choices":[{"delta":{"content":"pong"}}]}\n\ndata: [DONE]\n\n',
    })
  })

  await page.goto("/")
  await page.getByRole("button", { name: "连接模型" }).click()
  await page.getByLabel("接口地址").fill("http://127.0.0.1:9/v1")
  await page.getByLabel("接口类型").selectOption("responses")
  await page.getByRole("button", { name: "拉取模型" }).click()
  await expect(page.getByLabel("默认模型")).toHaveValue("unit-model")
  await page.getByRole("button", { name: "保存" }).click()

  await expect(page.getByRole("tab", { name: "对话" })).toHaveAttribute("aria-selected", "true")
  await expect(page.getByLabel("模型")).toBeVisible()
  await expect(page.getByLabel("思考强度")).toBeVisible()
  await page.getByLabel("思考强度").selectOption("high")
  await page.getByLabel("消息").fill("ping")
  await page.getByRole("button", { name: "发送" }).click()
  await expect(page.getByText("pong")).toBeVisible()
  expect(wire).toBe("responses")
})
