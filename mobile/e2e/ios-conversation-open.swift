import XCTest

// Copy into a disposable ios-layout-fixture with
// IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=hidden&pause=open'.
// Back must reopen the same host/thread, rather than preserve a previously
// paged answer and conceal a broken initial watch response.
final class BindFlowTests: XCTestCase {
  func testConversationReopenRetainsItsLiveEdgeAndEarlierHistory() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()

    for visit in 0...1 {
      let explanation = app.staticTexts["No visible messages in this turn yet. Open earlier history."]
      XCTAssertTrue(explanation.waitForExistence(timeout: 15),
        "The combined opening response must reach the hidden-history live edge")
      XCTAssertFalse(app.staticTexts["Earlier answer"].exists,
        "Reopening must start at the live edge instead of retaining the previous page")
      record(app, "conversation-open-\(visit)-live-edge")

      let earlier = app.buttons["Earlier"]
      XCTAssertTrue(earlier.waitForExistence(timeout: 5))
      XCTAssertTrue(earlier.isHittable)
      earlier.tap()
      XCTAssertTrue(app.staticTexts["Earlier answer"].waitForExistence(timeout: 8),
        "The stored answer must remain reachable through Earlier on every visit")
      XCTAssertFalse(explanation.exists)
      record(app, "conversation-open-\(visit)-earlier-answer")

      if visit == 0 {
        let back = app.buttons["Back"]
        XCTAssertTrue(back.isHittable)
        back.tap()
        let sameThread = app.buttons["Open Trim the layout pass"].firstMatch
        XCTAssertTrue(sameThread.waitForExistence(timeout: 8),
          "Back must return to the same offline host's conversation list")
        XCTAssertFalse(explanation.exists)
        record(app, "conversation-open-same-thread-inbox")
        XCTAssertTrue(sameThread.isHittable)
        sameThread.tap()
      }
    }
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "conversation-open-native-failure")
    }
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
