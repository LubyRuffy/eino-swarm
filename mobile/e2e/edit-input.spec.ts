import { expect, test } from "@playwright/test"

test("edits and resends a historical PC prompt with its original images", async ({ page }, info) => {
  await page.goto("/?mock=1&tick=0&history=input-images")
  const back = page.getByRole("button", { name: "返回", exact: true })
  await back.click()
  const thread = page.getByTestId("project-stack").getByRole("button", { name: "打开 Sweep the unused exports", exact: true })
  await thread.click()
  const edit = page.getByRole("button", { name: "编辑消息", exact: true })
  const editor = page.getByRole("textbox", { name: "编辑消息", exact: true })
  const resend = page.getByRole("button", { name: "重新发送", exact: true })
  const original = "Screenshots from the PC"

  // Steered inputs and assistant responses must never get a rewind control.
  await expect(edit).toHaveCount(1)
  await edit.tap()
  await expect(editor).toHaveValue(original)
  await editor.fill("Discard this edit")
  await page.getByRole("button", { name: "取消", exact: true }).tap()
  await expect(page.getByText(original, { exact: true })).toBeVisible()

  await edit.tap()
  await editor.fill("/goal update the objective")
  await resend.tap()
  await expect(page.getByRole("alert")).toContainText("输入框")
  await expect(editor).toHaveValue("/goal update the objective")
  await editor.fill("Please compare the two images.")
  await resend.tap()
  await expect(page.getByText("Please compare the two images.", { exact: true })).toBeVisible()
  await expect(page.getByText("Here is what changed:", { exact: true })).toBeVisible()
  await expect(page.getByText("I received both screenshots.", { exact: true })).toHaveCount(0)
  await expect(page.getByText(original, { exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "查看图片", exact: true })).toHaveCount(2)
  await page.screenshot({ path: info.outputPath("replacement.png"), animations: "disabled" })

  await back.click()
  await thread.click()
  await expect(page.getByText("Please compare the two images.", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "查看图片", exact: true })).toHaveCount(2)
  await expect(page.getByText(original, { exact: true })).toHaveCount(0)
})

test("an older PC asks for an update without sending a new message", async ({ page }) => {
  await page.goto("/?mock=1&tick=0&history=input-images&edit-input=legacy")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByTestId("project-stack").getByRole("button", { name: "打开 Sweep the unused exports", exact: true }).click()
  await page.getByRole("button", { name: "编辑消息", exact: true }).tap()
  await expect(page.getByRole("alert")).toContainText("更新")
  await expect(page.getByRole("button", { name: "重新发送", exact: true })).toBeDisabled()
  await page.getByRole("button", { name: "取消", exact: true }).tap()
  await expect(page.getByText("Screenshots from the PC", { exact: true })).toBeVisible()
  await expect(page.getByText("I received both screenshots.", { exact: true })).toBeVisible()
})
