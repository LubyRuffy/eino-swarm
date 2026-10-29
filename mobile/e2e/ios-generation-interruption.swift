import XCTest

// The disposable packaged fixture uses history=generation-interruption.
final class BindFlowTests: XCTestCase {
  func testInterruptedGenerationRetainsReasonAndRunningWorkerAfterReopen() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    for phase in ["first-open", "reopened"] {
      let agents = app.buttons["Agents (1)"]
      XCTAssertTrue(agents.waitForExistence(timeout: 20), "the same worker must remain active")
      agents.tap()
      let worker = app.buttons.matching(NSPredicate(format: "label CONTAINS 'worker-1'")).firstMatch
      XCTAssertTrue(worker.waitForExistence(timeout: 8))
      XCTAssertFalse(worker.label.lowercased().contains("failed"), "generation recovery cannot fail the worker")
      XCTAssertFalse(worker.label.lowercased().contains("stopped"))
      worker.tap()
      let reason = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Generation interrupted: engine restart.'")).firstMatch
      XCTAssertTrue(reason.waitForExistence(timeout: 8), "the old generation needs a visible cause")
      let recovery = app.staticTexts["Execution resumed; the interrupted output remains incomplete."]
      XCTAssertTrue(recovery.waitForExistence(timeout: 5), "recovery must not imply the interrupted output completed")
      let oldThought = app.buttons["Thought"].firstMatch
      XCTAssertTrue(oldThought.waitForExistence(timeout: 8), "the old thought must use a finished label")
      XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label == 'Thought'")).count, 2,
        "each generation has one disclosure, without extra work headers")
      XCTAssertFalse(app.staticTexts["Interrupted thought before restart."].exists)
      XCTAssertTrue(oldThought.isHittable)
      oldThought.tap()
      XCTAssertTrue(app.staticTexts["Interrupted thought before restart."].waitForExistence(timeout: 5))
      XCTAssertFalse(app.staticTexts["Recovery pending."].exists)
      record(app, phase)
      app.buttons["Back"].tap()
      XCTAssertTrue(worker.waitForExistence(timeout: 5))
      app.buttons["Back"].tap()
      XCTAssertTrue(agents.waitForExistence(timeout: 5))
    }
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 { record(XCUIApplication(), "failure") }
  }

  private func record(_ app: XCUIApplication, _ name: String) {
    let frame = XCTAttachment(screenshot: app.screenshot())
    frame.name = "generation-interruption-" + name
    frame.lifetime = .keepAlways
    add(frame)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = "generation-interruption-" + name + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
  }
}
