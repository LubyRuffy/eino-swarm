import { expect, test } from "@playwright/test"

// The existing completed walkthrough has a stable header, not a live ticker
// that could accidentally satisfy a reasoning-body assertion.
const thought = "Reading the current shape before touching it."

test.use({ trace: "retain-on-failure" })
test.afterEach(async ({ page }, info) => {
  if (info.status !== info.expectedStatus) {
    await page.screenshot({ path: info.outputPath("reasoning-fold-failure.png") })
  }
})

for (const language of [
  { locale: "en", back: "Back", open: "Open Sweep the unused exports", thought: "Thought" },
  { locale: "zh", back: "返回", open: "打开 Sweep the unused exports", thought: "思考" },
]) {
  test(`reasoning needs its own toggle after opening work (${language.locale})`, async ({ page }, info) => {
    const errors: string[] = []
    page.on("pageerror", (error) => errors.push(String(error)))
    await page.addInitScript((locale) => localStorage.setItem("zwai.phone.locale", locale), language.locale)
    await page.goto("/?mock=1&tick=0")
    await page.getByRole("button", { name: language.back, exact: true }).click()
    await page.getByRole("button", { name: language.open, exact: true }).click()

    const transcript = page.getByTestId("transcript")
    await expect(transcript.getByText("Here is what changed:", { exact: true })).toBeVisible()
    const work = transcript.getByTestId("work-fold")
    await expect(work).toHaveCount(1)
    await expect(work).toHaveAttribute("aria-expanded", "false")
    await work.click()

    const toggle = transcript.getByTestId("phone-thought-toggle")
    const body = transcript.getByTestId("phone-thought")
    await expect(work).toHaveAttribute("aria-expanded", "true")
    await expect(toggle).toHaveText(language.thought)
    await expect(toggle).toHaveAttribute("aria-expanded", "false")
    await expect(body).not.toBeVisible()
    await expect(transcript.getByText("read", { exact: true })).toBeVisible()
    await expect(transcript.getByText("exec", { exact: true })).toBeVisible()
    await page.screenshot({ path: info.outputPath(`work-open-thought-closed-${language.locale}.png`) })

    const controlled = await toggle.getAttribute("aria-controls")
    expect(controlled).toBeTruthy()
    await toggle.click()
    await expect(toggle).toHaveAttribute("aria-expanded", "true")
    await expect(body).toHaveAttribute("id", controlled!)
    await expect(body).toHaveJSProperty("textContent", thought)
    await expect(body).toBeVisible()
    await page.screenshot({ path: info.outputPath(`thought-open-${language.locale}.png`) })

    await toggle.click()
    await expect(toggle).toHaveAttribute("aria-expanded", "false")
    await expect(body).not.toBeVisible()
    await expect(work).toHaveAttribute("aria-expanded", "true")
    await expect(transcript.getByText("Here is what changed:", { exact: true })).toBeVisible()
    await page.screenshot({ path: info.outputPath(`thought-closed-again-${language.locale}.png`) })
    expect(errors).toEqual([])
  })
}
