import Foundation
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

  // These two methods use history=reasoning-only. The original mixed-tool
  // methods above retain the default walkthrough fixture and its outer fold.
  func testEnglishPureReasoningHasOneDisclosureAfterReopen() throws {
    try verifyPureReasoning(chinese: false)
  }

  func testChinesePureReasoningHasOneDisclosureAfterReopen() throws {
    try verifyPureReasoning(chinese: true)
  }

  private func verifyPureReasoning(chinese: Bool) throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    let thought = chinese ? "思考" : "Thought"
    let back = chinese ? "返回" : "Back"
    let recent = chinese ? "打开 Sweep the unused exports" : "Open Sweep the unused exports"
    let language = chinese ? "zh" : "en"
    _ = try uniqueButton(app, back, timeout: 15)

    func verify(_ phase: String, answerRequired: Bool, close: Bool) throws {
      let body = app.staticTexts.matching(identifier: reasoning)
      let toggle = try uniqueButton(app, thought)
      XCTAssertEqual(body.count, 0, "each mounted thought starts closed without opening another header")
      if answerRequired {
        let answer = app.staticTexts.matching(identifier: "Here is what changed:")
        XCTAssertTrue(answer.element(boundBy: 0).waitForExistence(timeout: 8))
        XCTAssertEqual(answer.count, 1)
      }
      record(app, "pure-" + language + "-" + phase + "-closed")
      toggle.tap()
      XCTAssertTrue(body.element(boundBy: 0).waitForExistence(timeout: 5), "one native tap must reveal the complete thought")
      XCTAssertEqual(body.count, 1)
      XCTAssertEqual(body.element(boundBy: 0).label, reasoning)
      XCTAssertTrue(body.element(boundBy: 0).isHittable)
      XCTAssertEqual(app.buttons.matching(identifier: thought).count, 1, "opening a pure thought must not expose a second disclosure")
      record(app, "pure-" + language + "-" + phase + "-open")
      if close {
        try uniqueButton(app, thought).tap()
        XCTAssertTrue(body.element(boundBy: 0).waitForNonExistence(timeout: 5))
        XCTAssertEqual(app.buttons.matching(identifier: thought).count, 1)
        record(app, "pure-" + language + "-" + phase + "-closed-again")
      }
    }

    try verify("live", answerRequired: false, close: true)
    try uniqueButton(app, back).tap()
    try uniqueButton(app, recent, timeout: 8).tap()
    try verify("completed", answerRequired: true, close: false)
    // Leave the completed thought open before navigating away. Reopening the
    // same conversation must remount its one disclosure in the closed state.
    try uniqueButton(app, back).tap()
    try uniqueButton(app, recent, timeout: 8).tap()
    try verify("completed-reopened", answerRequired: true, close: true)
    XCTAssertTrue(app.staticTexts["Here is what changed:"].exists)
  }

  private func uniqueButton(_ app: XCUIApplication, _ name: String, timeout: TimeInterval = 5) throws -> XCUIElement {
    let buttons = app.buttons.matching(identifier: name)
    let end = ProcessInfo.processInfo.systemUptime + timeout
    while ProcessInfo.processInfo.systemUptime < end {
      if buttons.count == 1 && buttons.element(boundBy: 0).isHittable && buttons.element(boundBy: 0).isEnabled {
        return buttons.element(boundBy: 0)
      }
      RunLoop.current.run(until: Date().addingTimeInterval(0.05))
    }
    record(app, "unique-button-refusal-" + name)
    throw NSError(domain: "NativeReasoningDisclosure", code: 1,
                  userInfo: [NSLocalizedDescriptionKey: "Missing, ambiguous or unavailable native button: " + name])
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
