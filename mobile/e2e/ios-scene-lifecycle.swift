import XCTest

// Run through ios-layout-fixture.py on a fresh private iOS 27.2 simulator.
// Keep the candidate's public bytes intact. The offline case uses
// /?mock=1&tick=0; the account case uses / and the current signed-out bundle.
final class BindFlowTests: XCTestCase {
  func testNativeSceneColdStartBackgroundReturnAndRelaunch() throws {
    continueAfterFailure = false
    try exerciseLifecycle { app, stage in
      if stage != "foreground-return" {
        let ready = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
          app.buttons["Back"].firstMatch.exists || app.buttons["Menu"].firstMatch.exists
        }, object: nil)
        XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: 15), .completed,
          "The packaged offline page must replace the launch screen")
        // A cold launch intentionally resumes the live mock conversation.
        // Reach its inbox through the real Back control before observing it.
        let back = app.buttons["Back"].firstMatch
        if back.exists {
          XCTAssertTrue(back.isHittable)
          back.tap()
        }
      }
      let menu = app.buttons["Menu"].firstMatch
      let newChat = app.buttons["New chat"].firstMatch
      let host = app.descendants(matching: .any)
        .matching(NSPredicate(format: "label == %@", "Walkthrough PC")).firstMatch
      XCTAssertTrue(menu.waitForExistence(timeout: 10), "The native scene must display the inbox menu")
      XCTAssertTrue(newChat.waitForExistence(timeout: 5), "The inbox must display its New chat control")
      XCTAssertTrue(host.waitForExistence(timeout: 5), "The saved offline PC must be rendered")
      XCTAssertTrue(menu.isHittable)
      XCTAssertTrue(newChat.isHittable)
      record(app, "scene-offline-" + stage)

      // A visible process or stale screenshot cannot prove that the resumed
      // WebView handles native input. Open and close its actual menu.
      menu.tap()
      let version = app.staticTexts
        .matching(NSPredicate(format: "label BEGINSWITH 'Version '")).firstMatch
      XCTAssertTrue(version.waitForExistence(timeout: 5), "The menu must respond to a native tap")
      record(app, "scene-offline-menu-" + stage)
      let close = app.buttons["Close"].firstMatch
      XCTAssertTrue(close.isHittable)
      close.tap()
      XCTAssertTrue(version.waitForNonExistence(timeout: 5))
      XCTAssertTrue(newChat.isHittable)
    }
  }

  func testNativeSceneFreshAccountPageSurvivesBackgroundReturnAndRelaunch() throws {
    continueAfterFailure = false
    try exerciseLifecycle { app, stage in
      let login = app.buttons["Sign in"].firstMatch
      XCTAssertTrue(login.waitForExistence(timeout: 15),
        "A fresh current bundle must paint its signed-out account entry")
      XCTAssertTrue(login.isHittable)
      XCTAssertFalse(app.textFields["API key"].exists)
      record(app, "scene-account-" + stage)
    }
  }

  private func exerciseLifecycle(_ verify: (XCUIApplication, String) -> Void) throws {
    let app = XCUIApplication()
    app.launch()
    XCTAssertTrue(app.wait(for: .runningForeground, timeout: 8))
    verify(app, "cold-start")

    XCUIDevice.shared.press(.home)
    let background = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
      app.state == .runningBackground || app.state == .runningBackgroundSuspended
    }, object: nil)
    XCTAssertEqual(XCTWaiter.wait(for: [background], timeout: 8), .completed,
      "Home must actually move the scene out of the foreground")
    let state = XCTAttachment(string: "applicationState=\(app.state.rawValue)")
    state.name = "scene-background-state"
    state.lifetime = .keepAlways
    add(state)

    app.activate()
    XCTAssertTrue(app.wait(for: .runningForeground, timeout: 8))
    verify(app, "foreground-return")

    app.terminate()
    XCTAssertTrue(app.wait(for: .notRunning, timeout: 5), "Relaunch must follow a real process exit")
    app.launch()
    XCTAssertTrue(app.wait(for: .runningForeground, timeout: 8))
    verify(app, "cold-relaunch")
  }

  override func tearDownWithError() throws {
    let app = XCUIApplication()
    if (testRun?.failureCount ?? 0) > 0 {
      let screen = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
      screen.name = "scene-lifecycle-failure-screen"
      screen.lifetime = .keepAlways
      add(screen)
      if app.state == .runningForeground { record(app, "scene-lifecycle-failure") }
    }
    app.terminate()
    XCTAssertTrue(app.wait(for: .notRunning, timeout: 5), "The case must leave its packaged app exited")
  }

  private func record(_ app: XCUIApplication, _ name: String) {
    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = name
    screenshot.lifetime = .keepAlways
    add(screenshot)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = name + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
  }
}
