import { expect, test } from "@playwright/test"

// Exercise the actual phone composer, HTTP reader and persisted transcript.
// The endpoint is intercepted locally; these checks never use a real model.
for (const field of ["reasoning_content", "reasoning"] as const) {
  for (const wire of ["SSE", "JSON"] as const) {
    test(`${field} from ${wire} retains its own collapsed control after reopening a direct chat`, async ({ page }) => {
      const pieces = ["检查当前证据 before answering.\n", "Compare both paths before concluding."]
      const thought = pieces.join("")
      const answer = "Verified answer."
      const prompt = "Inspect the current input."
      let requests = 0
      await page.route("http://127.0.0.1:9/**", async (route) => {
        const url = route.request().url()
        if (url.endsWith("/models")) {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({ data: [{ id: "fixture-model" }] }),
          })
          return
        }
        expect(url).toBe("http://127.0.0.1:9/v1/chat/completions")
        requests++
        const sent = route.request().postDataJSON() as { messages: { role: string; content: string }[] }
        expect(sent.messages).toEqual([{ role: "user", content: prompt }])
        const message = { role: "assistant", content: answer, [field]: thought }
        await route.fulfill({
          status: 200,
          contentType: wire === "JSON" ? "application/json" : "text/event-stream",
          body: wire === "JSON"
            ? JSON.stringify({ choices: [{ message, finish_reason: "stop" }] })
            : [
              ...pieces.map((text) => ({ choices: [{ delta: { role: "assistant", [field]: text } }] })),
              { choices: [{ delta: { content: answer }, finish_reason: "stop" }] },
            ].map((frame) => `data: ${JSON.stringify(frame)}\n\n`).join("") + "data: [DONE]\n\n",
        })
      })

      await page.goto("/")
      await page.getByRole("button", { name: "连接模型" }).click()
      await page.getByLabel("接口地址").fill("http://127.0.0.1:9/v1")
      await page.getByLabel("接口类型").selectOption("chat")
      await page.getByRole("button", { name: "拉取模型" }).click()
      await expect(page.getByLabel("默认模型")).toHaveValue("fixture-model")
      await page.getByRole("button", { name: "保存" }).click()
      await page.getByLabel("消息").fill(prompt)
      await page.getByRole("button", { name: "发送" }).click()

      await expect(page.getByText(answer, { exact: true })).toBeVisible()
      await expect(page.getByTestId("work-fold")).toHaveCount(1)
      await page.getByTestId("work-fold").click()
      await expect(page.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
      await expect(page.getByTestId("phone-thought")).not.toBeVisible()
      await page.getByTestId("phone-thought-toggle").click()
      await expect(page.getByTestId("phone-thought")).toHaveJSProperty("textContent", thought)
      expect(requests).toBe(1)

      await page.reload()
      await page.getByRole("button", { name: `打开 ${prompt}`, exact: true }).click()
      await expect(page.getByText(answer, { exact: true })).toBeVisible()
      await expect(page.getByTestId("work-fold")).toHaveCount(1)
      await page.getByTestId("work-fold").click()
      await expect(page.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
      await expect(page.getByTestId("phone-thought")).not.toBeVisible()
      await page.getByTestId("phone-thought-toggle").click()
      await expect(page.getByTestId("phone-thought")).toHaveJSProperty("textContent", thought)
      expect(requests).toBe(1)
    })
  }
}
