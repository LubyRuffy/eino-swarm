import { expect, test } from "@playwright/test"

test("phone settings shows a pairing QR control", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByLabel("Hub URL")).toBeVisible()
  await expect(dialog.getByLabel("Host Token")).toHaveCount(0)
  await expect(dialog.getByLabel("Event text on the phone")).toBeVisible()
  await expect(dialog.getByLabel("Events on the phone")).toBeVisible()
  await expect(dialog.getByLabel("This computer's name")).toBeVisible()
  await expect(dialog.getByLabel("Keep this computer awake")).toBeVisible()
  await expect(dialog.getByText("Bound phones")).toBeVisible()
  await expect(dialog.getByText("No phones bound yet.")).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Show pairing QR" })).toBeVisible()
  await dialog.getByRole("button", { name: "Show pairing QR" }).click()
  const toast = page.getByRole("alert")
  await expect(toast).toContainText("Couldn't set up the phone")
  await expect(toast).toBeInViewport()
  await expect(page.getByTestId("remote-qr")).toHaveCount(0)
  await toast.getByRole("button", { name: "Dismiss" }).click()
  await expect(toast).toHaveCount(0)
})

test("a phone that binds while the QR stays up shows up without leaving Phone", async ({
  page,
}) => {
  let offered = false
  let afterOffer = 0
  await page.route("**/api/remote/offer", async (route) => {
    offered = true
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        uri: "pairlink:v1:http://127.0.0.1:9:code:spk",
        pairing_id: "p1",
        expires_at: new Date(Date.now() + 60_000).toISOString(),
        png: "data:image/png;base64,aaaa",
      }),
    })
  })
  await page.route("**/api/remote/bindings", async (route) => {
    if (offered) afterOffer += 1
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        bindings:
          !offered || afterOffer < 2
            ? []
            : [
                {
                  id: "b-new",
                  device_fp: "cc22dd33ee44ff55",
                  device: "Fresh Phone",
                  created_at: "2026-09-24T01:00:00Z",
                  last_seen: "2026-09-24T01:00:02Z",
                  session_id: "s-new",
                },
              ],
      }),
    })
  })
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByText("No phones bound yet.")).toBeVisible()
  await dialog.getByRole("button", { name: "Show pairing QR" }).click()
  await expect(page.getByTestId("remote-qr")).toBeVisible()
  await expect(dialog.getByText("Fresh Phone")).toBeVisible()
  await expect(dialog.getByText("No phones bound yet.")).toHaveCount(0)
})

test("bound phones paints the reported model instead of a bare fingerprint", async ({
  page,
}) => {
  await page.route("**/api/remote/bindings", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        bindings: [
          {
            id: "b1",
            device_fp: "aa11bb22cc33dd44",
            device: "Phone 1.0 Device",
            created_at: "2026-09-20T16:00:00Z",
            last_seen: "2026-09-20T16:03:32Z",
            session_id: "s1",
          },
        ],
      }),
    })
  })
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByText("Phone 1.0 Device")).toBeVisible()
  await expect(dialog.getByText(/aa11bb22cc33dd44/)).toBeVisible()
  await expect(dialog.getByText(/Last connected/)).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Revoke" })).toBeVisible()
})
