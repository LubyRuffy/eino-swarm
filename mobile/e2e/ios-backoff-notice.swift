import XCTest

/// The disposable fixture selects history=answer-backoff in the packaged assets.
final class BindFlowTests: XCTestCase {
  func testEnglishCandidateRecoveryAndOriginCooldownStayDistinctAfterReopen() throws {
    try verifyNotices(chinese: false)
  }

  func testChineseCandidateRecoveryAndOriginCooldownStayDistinctAfterReopen() throws {
    try verifyNotices(chinese: true)
  }

  private func verifyNotices(chinese: Bool) throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons[chinese ? "返回" : "Back"]
    XCTAssertTrue(back.waitForExistence(timeout: 20), "packaged offline conversation must use the explicitly requested locale")
    let notices = chinese ? [
      "等待 5 秒后重试模型请求。", "等待 10 秒后重试模型请求。",
      "等待 5 秒后重试未发布答复的核对。", "答复核对暂不可用，未发布候选已保留。"
    ] : [
      "Waiting 5s before retrying the model request.", "Waiting 10s before retrying the model request.",
      "Waiting 5s before checking the unpublished answer again.",
      "Answer verification is unavailable. The unpublished answer is retained."
    ]
    for phase in ["first-open", "reopened"] {
      XCTAssertTrue(app.staticTexts[notices[3]].waitForExistence(timeout: 10))
      for text in notices {
        XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == %@", text)).count, 1,
          "each cooldown/candidate notice must be shown exactly once")
      }
      for forbidden in [
        "Waiting before retrying the model request.", "Waiting 4s before retrying the model request.",
        "等待后重试模型请求。", "等待 4 秒后重试模型请求。",
        "schedule-answer-policy", "private-answer", "wait_ms", "Agents (", "子 Agent ("
      ] {
        XCTAssertFalse(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", forbidden)).firstMatch.exists,
          "bookkeeping cannot become another notice, raw payload or classifier worker")
      }
      record(app, phase)
      back.tap()
      let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Trim the layout pass'")).firstMatch
      XCTAssertTrue(thread.waitForExistence(timeout: 8))
      thread.tap()
      XCTAssertTrue(back.waitForExistence(timeout: 8))
    }
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 { record(XCUIApplication(), "failure") }
  }

  private func record(_ app: XCUIApplication, _ phase: String) {
    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "backoff-notice-" + phase
    screenshot.lifetime = .keepAlways
    add(screenshot)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = "backoff-notice-" + phase + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
  }
}
