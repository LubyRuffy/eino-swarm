import XCTest

// Copy into the disposable ios-layout-fixture with history=active-unread.
// Native taps stay on Home so opening a task cannot acknowledge its result.
final class BindFlowTests: XCTestCase {
  func testNativeActiveUnreadHomeProjectionKeepsOldWatermarks() throws {
    continueAfterFailure = false
    print("Issue83 native driver: case-insensitive-headings-v3")
    let app = XCUIApplication()
    app.launch()
    XCTAssertTrue(heading(app, "In progress").waitForExistence(timeout: 20))
    XCTAssertFalse(app.buttons["Back"].exists)
    XCTAssertTrue(waitUntil { self.rows(app, "Shared task").count == 1 })
    XCTAssertEqual(rows(app, "Pending answer").count, 0)
    XCTAssertEqual(rows(app, "Scheduled task").count, 0)
    record(app, "active-unread-first-page")

    let more = app.buttons["More"].firstMatch
    XCTAssertTrue(more.isHittable)
    more.tap()
    XCTAssertTrue(waitUntil { self.rows(app, "Completed item 2").count == 1 })
    XCTAssertEqual(rows(app, "Shared task").count, 1)
    XCTAssertEqual(rows(app, "Pending answer").count, 0)
    XCTAssertEqual(rows(app, "Scheduled task").count, 0)
    record(app, "active-unread-expanded-live")

    XCTAssertTrue(app.buttons["More"].firstMatch.isHittable)
    app.buttons["More"].firstMatch.tap()
    XCTAssertTrue(waitUntil(timeout: 20) { self.rows(app, "Shared task").count == 2 },
      "The two different identities must retain their old unread results after live status ends")
    XCTAssertEqual(rows(app, "Pending answer").count, 1)
    XCTAssertEqual(rows(app, "Scheduled task").count, 1)
    XCTAssertFalse(app.buttons["Back"].exists, "No detail visit may send a legitimate read ACK")
    XCTAssertTrue(waitUntil { self.rows(app, "Completed item 4").count == 1 })
    XCTAssertEqual(attentionButtons(app).count, 8)
    record(app, "active-unread-idle-watermarks-retained")
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "active-unread-failure")
    }
  }

  private func rows(_ app: XCUIApplication, _ title: String) -> [XCUIElement] {
    attentionButtons(app).filter { $0.label == "Open \(title)" }
  }

  private func attentionButtons(_ app: XCUIApplication) -> [XCUIElement] {
    let completed = heading(app, "Completed unread")
    guard completed.exists else { return [] }
    let start = completed.frame.maxY
    let following = ["In progress", "Platform", "Field notes", "Recent"]
      .compactMap { label -> CGFloat? in
        let element = heading(app, label)
        return element.exists && element.frame.minY >= start ? element.frame.minY : nil
      }.min() ?? CGFloat.greatestFiniteMagnitude
    return app.buttons.allElementsBoundByIndex.filter {
      $0.label.hasPrefix("Open ") && $0.frame.midY > start && $0.frame.midY < following
    }
  }

  private func heading(_ app: XCUIApplication, _ label: String) -> XCUIElement {
    // WebKit exposes the CSS-uppercase text through native accessibility.
    app.staticTexts.matching(NSPredicate(format: "label ==[c] %@", label)).firstMatch
  }

  private func waitUntil(timeout: TimeInterval = 8, _ predicate: @escaping () -> Bool) -> Bool {
    let reached = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in predicate() }, object: nil)
    return XCTWaiter.wait(for: [reached], timeout: timeout) == .completed
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
