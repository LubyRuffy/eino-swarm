import XCTest

// Copy into the disposable ios-layout-fixture with /?mock=1&tick=0.
// These assertions cover the packaged composer. MockLink does not prove goal
// persistence; the real server/remote goal integration test covers that boundary.
final class BindFlowTests: XCTestCase {
  private let refusal = "The /goal and /plan commands accept text only. Remove quotations first. Your draft was kept."

  func testQuotedGoalKeepsDraftUntilQuoteIsRemoved() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    try addNativeQuote(app)
    let quote = app.textViews["Edit quote 1"].firstMatch
    let selected = try XCTUnwrap(quote.value as? String)
    XCTAssertFalse(selected.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
    let command = "/goal Inspect the assigned operation Issue111"
    let input = app.textViews["Message"]
    input.tap()
    input.typeText(command)
    XCTAssertFalse(hasTranscriptText(app, command))
    app.buttons["Send"].tap()

    XCTAssertTrue(app.staticTexts[refusal].waitForExistence(timeout: 5))
    XCTAssertEqual(input.value as? String, command, "Refusal must retain the complete command")
    XCTAssertEqual(quote.value as? String, selected, "Refusal must retain the selected quotation")
    XCTAssertFalse(hasTranscriptText(app, command), "A rejected command must not become an ordinary user bubble")
    record(app, "quoted-goal-refused-draft-kept")

    let remove = app.buttons["Remove quote 1"]
    XCTAssertTrue(remove.isHittable)
    remove.tap()
    XCTAssertTrue(quote.waitForNonExistence(timeout: 5))
    XCTAssertFalse(app.staticTexts[refusal].exists)
    XCTAssertEqual(input.value as? String, command)
    app.buttons["Send"].tap()
    XCTAssertTrue(app.staticTexts[command].waitForExistence(timeout: 10),
      "After removing the quote, the complete plain command must reach the mock transcript")
    XCTAssertTrue(["", "Message"].contains(input.value as? String ?? ""))
    record(app, "plain-goal-original-text-submitted")
  }

  func testOrdinaryQuotedMessageStillSends() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    try addNativeQuote(app)
    let input = app.textViews["Message"]
    let body = "Explain the selected context Issue111"
    input.tap()
    input.typeText(body)
    app.buttons["Send"].tap()
    XCTAssertTrue(app.staticTexts[body].waitForExistence(timeout: 10))
    XCTAssertTrue(app.staticTexts["Selected text"].firstMatch.exists,
      "The ordinary message must retain its separate quotation")
    XCTAssertFalse(app.textViews["Edit quote 1"].exists)
    XCTAssertFalse(app.staticTexts[refusal].exists)
    XCTAssertTrue(["", "Message"].contains(input.value as? String ?? ""))
    record(app, "ordinary-quoted-message-submitted")
  }

  private func addNativeQuote(_ app: XCUIApplication) throws {
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    XCTAssertTrue(thread.isHittable)
    thread.tap()
    let answer = app.staticTexts["Here is what changed:"].firstMatch
    XCTAssertTrue(answer.waitForExistence(timeout: 8))
    answer.press(forDuration: 1.2)
    let add = app.buttons["Add to chat"]
    XCTAssertTrue(add.waitForExistence(timeout: 5))
    XCTAssertTrue(add.isHittable, "The native selection menu must leave Add to chat reachable")
    record(app, "native-selection-before-add-to-chat")
    add.tap()
    XCTAssertTrue(app.textViews["Edit quote 1"].firstMatch.waitForExistence(timeout: 5))
  }

  private func hasTranscriptText(_ app: XCUIApplication, _ text: String) -> Bool {
    // WKWebView also exposes a textarea value as nested StaticText. Only a
    // matching element above the composer is a sent transcript bubble.
    let top = app.textViews["Message"].frame.minY
    return app.staticTexts.matching(NSPredicate(format: "label == %@", text))
      .allElementsBoundByIndex.contains { $0.frame.maxY <= top }
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 { record(XCUIApplication(), "quoted-command-native-failure") }
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
