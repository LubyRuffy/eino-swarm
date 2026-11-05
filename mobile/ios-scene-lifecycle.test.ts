import { readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"

const root = path.dirname(fileURLToPath(import.meta.url))
const read = (relative: string) => readFileSync(path.join(root, relative), "utf8")

function plistValue(element: Element): unknown {
  if (element.tagName === "dict") {
    const values: Record<string, unknown> = {}
    const entries = Array.from(element.children)
    for (let index = 0; index < entries.length; index += 2) {
      values[entries[index].textContent ?? ""] = plistValue(entries[index + 1])
    }
    return values
  }
  if (element.tagName === "array") return Array.from(element.children, plistValue)
  if (element.tagName === "true") return true
  if (element.tagName === "false") return false
  return element.textContent
}

describe("iOS scene lifecycle", () => {
  it("opens the existing streaming bridge through a single scene on iOS 27", () => {
    // UIKit rejects apps linked with the iOS 27 SDK before rendering without
    // scene adoption. The scene must retain our custom bridge and its plugins.
    const xml = new DOMParser().parseFromString(read("ios/App/App/Info.plist"), "application/xml")
    const plist = plistValue(xml.documentElement.firstElementChild!) as Record<string, unknown>
    expect(plist.UIApplicationSceneManifest).toEqual({
      UIApplicationSupportsMultipleScenes: false,
      UISceneConfigurations: {
        UIWindowSceneSessionRoleApplication: [{
          UISceneConfigurationName: "Default Configuration",
          UISceneDelegateClassName: "$(PRODUCT_MODULE_NAME).SceneDelegate",
          UISceneStoryboardFile: "Main",
        }],
      },
    })
    expect(plist).not.toHaveProperty("UIMainStoryboardFile")
    expect(plist.UILaunchStoryboardName).toBe("LaunchScreen")

    const storyboard = new DOMParser().parseFromString(
      read("ios/App/App/Base.lproj/Main.storyboard"), "application/xml",
    )
    const initial = storyboard.documentElement.getAttribute("initialViewController")
    expect(storyboard.querySelector(`[id="${initial}"]`)?.getAttribute("customClass"))
      .toBe("BridgeViewController")
    const scene = read("ios/App/App/SceneDelegate.swift")
    expect(scene).toMatch(/class SceneDelegate: UIResponder, UIWindowSceneDelegate/)
    expect(scene).toMatch(/var window: UIWindow\?/)
    // The scene storyboard already owns the window; a second bridge would
    // duplicate the page, model requests and native plugin registrations.
    expect(scene).not.toMatch(/UIWindow\(|rootViewController\s*=|makeKeyAndVisible/)
    expect(scene).not.toMatch(/NotificationCenter/)
  })

  it("compiles the scene delegate into the actual App target", () => {
    const project = read("ios/App/App.xcodeproj/project.pbxproj")
    const reference = project.match(/([A-F0-9]{24}) \/\* SceneDelegate\.swift \*\/ = \{isa = PBXFileReference;[^\n]*path = SceneDelegate\.swift;/)?.[1]
    expect(reference, "SceneDelegate has no project file reference").toBeTruthy()
    const buildFile = project.match(new RegExp(`([A-F0-9]{24}) /\\* SceneDelegate\\.swift in Sources \\*/ = \\{isa = PBXBuildFile; fileRef = ${reference}`))?.[1]
    expect(buildFile, "SceneDelegate has no build source entry").toBeTruthy()
    const appSources = project.match(/504EC3001FED79650016851F \/\* Sources \*\/ = \{[\s\S]*?files = \(([\s\S]*?)\);/)?.[1]
    expect(appSources).toContain(`${buildFile} /* SceneDelegate.swift in Sources */`)
    const appGroup = project.match(/504EC3061FED79650016851F \/\* App \*\/ = \{[\s\S]*?children = \(([\s\S]*?)\);/)?.[1]
    expect(appGroup).toContain(`${reference} /* SceneDelegate.swift */`)
  })
})
