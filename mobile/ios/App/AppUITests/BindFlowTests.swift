import XCTest

/// Drives the packaged Capacitor app on a simulator. Put the mint from
/// POST /api/remote/offer on the simulator pasteboard first:
/// `xcrun simctl pbcopy booted < offer.txt`
final class BindFlowTests: XCTestCase {
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
