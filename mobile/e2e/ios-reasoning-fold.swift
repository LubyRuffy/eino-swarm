import XCTest

// Copy into ios-layout-fixture with the unchanged current public resources,
// mock=1&tick=0 and the locale matching the selected test method.
// These are real packaged UI actions, not a host/model or release acceptance.
final class BindFlowTests: XCTestCase {
  private let reasoning = "Reading the current shape before touching it."

  func testEnglishReasoningRequiresItsOwnToggle() throws {
    try verifyReasoning(chinese: false)
  }

  func testChineseReasoningRequiresItsOwnToggle() throws {
    try verifyReasoning(chinese: true)
  }

  private func verifyReasoning(chinese: Bool) throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons[chinese ? "返回" : "Back"]
    XCTAssertTrue(back.waitForExistence(timeout: 15))
    XCTAssertTrue(back.isHittable)
    back.tap()
    let recent = app.buttons[chinese ? "打开 Sweep the unused exports" : "Open Sweep the unused exports"]
    XCTAssertTrue(recent.waitForExistence(timeout: 8))
    XCTAssertTrue(recent.isHittable)
    recent.tap()

    let answer = app.staticTexts["Here is what changed:"].firstMatch
    XCTAssertTrue(answer.waitForExistence(timeout: 8))
    let work = app.buttons[chinese ? "思考 · 2 个工具" : "Thought · 2 tools"]
    XCTAssertTrue(work.waitForExistence(timeout: 5))
    XCTAssertTrue(work.isHittable)
    work.tap()

    let toggle = app.buttons[chinese ? "思考" : "Thought"]
    XCTAssertTrue(toggle.waitForExistence(timeout: 5), "opening work must expose its separate thought control")
    XCTAssertTrue(toggle.isHittable)
    let body = app.staticTexts[reasoning].firstMatch
    XCTAssertFalse(body.exists, "opening work must not reveal reasoning automatically")
    record(app, "work-open-thought-closed")

    toggle.tap()
    XCTAssertTrue(body.waitForExistence(timeout: 5), "the real second-level tap must reveal the original full thought")
    XCTAssertTrue(body.isHittable)
    XCTAssertEqual(body.label, reasoning)
    record(app, "thought-open")

    XCTAssertTrue(toggle.isHittable)
    toggle.tap()
    XCTAssertTrue(body.waitForNonExistence(timeout: 5), "the same control must close the thought again")
    XCTAssertTrue(work.exists)
    XCTAssertTrue(answer.exists)
    record(app, "thought-closed-again")
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "failure")
    }
  }

  private func record(_ app: XCUIApplication, _ name: String) {
    // AX assertions do not establish displayed pixels: inspect these actual
    // attachments before accepting the native run, including the closed stage.
    let frame = XCTAttachment(screenshot: app.screenshot())
    frame.name = "reasoning-fold-" + name
    frame.lifetime = .keepAlways
    add(frame)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = "reasoning-fold-" + name + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
  }
}
