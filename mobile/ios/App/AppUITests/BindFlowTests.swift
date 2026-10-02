import XCTest

/// Drives the packaged Capacitor app on a simulator. Put the mint from
/// POST /api/remote/offer on the simulator pasteboard first:
/// `xcrun simctl pbcopy booted < offer.txt`
final class BindFlowTests: XCTestCase {
  func testNativeIOSMenuHidesAndroidUpdateCheck() throws {
    let app = XCUIApplication()
    app.launch()

    let menu = app.buttons["Menu"]
    if !menu.waitForExistence(timeout: 3) {
      let connect = app.buttons["Connect a model"]
      XCTAssertTrue(connect.waitForExistence(timeout: 12), "fresh iOS install did not reach the scan screen")
      connect.tap()

      let baseURL = app.textFields["Base URL"]
      XCTAssertTrue(baseURL.waitForExistence(timeout: 5), "model form did not open")
      baseURL.tap()
      baseURL.typeText("http://127.0.0.1:1/v1")
      XCTAssertEqual(baseURL.value as? String, "http://127.0.0.1:1/v1")
      app.toolbars.buttons["Done"].tap()
      app.webViews.buttons["Save"].tap()
    }
    XCTAssertTrue(menu.waitForExistence(timeout: 8), "local chat did not open")
    menu.tap()
    XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Version '")).firstMatch.waitForExistence(timeout: 5), "menu did not open")
    XCTAssertFalse(app.descendants(matching: .any).matching(NSPredicate(format: "label == 'Check for updates'")).firstMatch.exists, "iOS menu offered the Android installer")
    let menuScreenshot = XCTAttachment(screenshot: app.screenshot())
    menuScreenshot.name = "iOS menu without Android update check"
    menuScreenshot.lifetime = .keepAlways
    add(menuScreenshot)
  }

  func testPasteBindListsTheSeedConversation() throws {
    let app = XCUIApplication()
    app.launch()

    let field = app.textViews["Pairing URI"]
    if field.waitForExistence(timeout: 4) {
      field.tap()
      field.press(forDuration: 1.2)
      let paste = app.menuItems["Paste"].firstMatch
      try XCTSkipIf(!paste.waitForExistence(timeout: 4), "simulator pasteboard empty — pbcopy a pairlink URI")
      paste.tap()
      app.buttons["Paste and bind"].tap()
    }

    // A live or last thread opens immediately; the inbox is behind Back.
    let path = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'path='")).firstMatch
    let back = app.buttons["Back"]
    let deadline = Date().addingTimeInterval(25)
    var landed = false
    while Date() < deadline {
      if path.exists || back.exists {
        landed = true
        break
      }
      RunLoop.current.run(until: Date().addingTimeInterval(0.2))
    }
    XCTAssertTrue(landed, "bind did not reach the home screen or a thread")
    if back.exists {
      back.tap()
    }
    XCTAssertTrue(path.waitForExistence(timeout: 8), "bind did not reach the home screen")
    // WKWebView exposes the Recent row as a Button, not a StaticText.
    let seed = app.buttons.matching(NSPredicate(format: "label CONTAINS 'sim-flow'")).firstMatch
    XCTAssertTrue(seed.waitForExistence(timeout: 8), "seed conversation missing after bind")

    // Starting is its own screen now: the inbox only reads and finds.
    let newChat = app.buttons["New chat"].firstMatch
    XCTAssertTrue(newChat.waitForExistence(timeout: 4), "New chat missing on the inbox")
    newChat.tap()
    let compose = app.textFields["New message"]
    XCTAssertTrue(compose.waitForExistence(timeout: 4), "compose field missing")
    compose.tap()
    compose.typeText("sim-ios")
    app.buttons["Start"].tap()
    XCTAssertTrue(app.buttons["Back"].waitForExistence(timeout: 15), "start did not open a thread")
    let action = app.buttons.matching(
      NSPredicate(format: "label == 'Stop' OR label == 'Follow-up' OR label == 'Send'")
    ).firstMatch
    XCTAssertTrue(action.waitForExistence(timeout: 12), "thread actions missing after start")
  }
}
