import XCTest

// English offline fixture: /?mock=1&ask=1&askfail=1&tick=0.
final class BindFlowTests: XCTestCase {
  func testAskChoicesRetainAnswerAndNativeViewportAcrossFocusAndRetry() throws {
    var boundsFailures: [String] = []
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    XCTAssertTrue(app.buttons["Back"].firstMatch.waitForExistence(timeout: 20))
    XCTAssertTrue(app.staticTexts["When should this run?"].waitForExistence(timeout: 10))
    for option in ["Now", "Later", "Other"] {
      let button = app.buttons[option].firstMatch
      XCTAssertTrue(button.waitForExistence(timeout: 5))
      XCTAssertEqual(button.label, option, "the native choice contains only its readable label")
    }
    XCTAssertFalse(app.buttons["now · Now"].exists)
    XCTAssertFalse(app.buttons["later · Later"].exists)
    record(app, "01-Ask-labels-without-identities")
    app.buttons["Other"].firstMatch.tap()
    let text = app.textViews["Other"].firstMatch
    XCTAssertTrue(text.waitForExistence(timeout: 5))
    let submit = app.buttons["Submit"].firstMatch
    XCTAssertFalse(submit.isEnabled)
    let answer = "Independent follow-up."
    text.tap()
    app.typeText(answer)
    XCTAssertEqual(text.value as? String, answer)
    checkViewport(app, phase: "02-focused-Other-with-keyboard", requireHeader: false, failures: &boundsFailures)
    if app.keyboards.firstMatch.exists {
      let done = try XCTUnwrap(app.toolbars.buttons.allElementsBoundByIndex.last)
      XCTAssertTrue(done.isHittable); done.tap()
      XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 5))
    }
    checkViewport(app, phase: "03-after-keyboard-Done", requireHeader: true, failures: &boundsFailures)
    XCTAssertTrue(submit.isHittable)
    submit.tap()
    let rejection = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "PC rejected the answer")).firstMatch
    XCTAssertTrue(rejection.waitForExistence(timeout: 8))
    XCTAssertEqual(text.value as? String, answer)
    XCTAssertTrue(submit.isEnabled)
    checkViewport(app, phase: "04-rejected-answer-retained", requireHeader: true, failures: &boundsFailures)
    submit.tap()
    XCTAssertTrue(rejection.waitForNonExistence(timeout: 8))
    XCTAssertTrue(text.waitForNonExistence(timeout: 8))
    XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "The answer was received.")).firstMatch.waitForExistence(timeout: 8))
    checkViewport(app, phase: "05-Ask-retry-received", requireHeader: true, failures: &boundsFailures)
    XCTAssertTrue(boundsFailures.isEmpty, "Native layout exceeded the actual App window: " + boundsFailures.joined(separator: "; "))
  }
  // Collect every actual phase before asserting so a functional green cannot
  // hide the focus, keyboard-Done, refusal, or success viewport failure.
  private func checkViewport(_ app: XCUIApplication, phase: String, requireHeader: Bool, failures: inout [String]) {
    let main = app.otherElements.matching(NSPredicate(format: "label == %@", "main")).firstMatch
    let back = app.buttons["Back"].firstMatch
    let window = app.frame
    let exists = main.waitForExistence(timeout: 5)
    let mainFrame = main.frame
    let backFrame = back.frame
    let horizontalFits = exists && mainFrame.width > 0 && mainFrame.minX >= window.minX - 1 && mainFrame.maxX <= window.maxX + 1
    let headerFits = !requireHeader || (back.exists && backFrame.minY >= window.minY - 1 && backFrame.maxY <= window.maxY + 1 && back.isHittable)
    let result = "phase=\(phase) window=\(window) main=\(mainFrame) back=\(backFrame) keyboard=\(app.keyboards.firstMatch.exists) horizontalFits=\(horizontalFits) headerFits=\(headerFits)"
    let bounds = XCTAttachment(string: result)
    bounds.name = "phone-ask-" + phase + "-actual-window-bounds"
    bounds.lifetime = .keepAlways; add(bounds)
    record(app, phase)
    if !horizontalFits || !headerFits { failures.append(result) }
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 { record(XCUIApplication(), "failure") }
    XCUIApplication().terminate()
  }
  private func record(_ app: XCUIApplication, _ phase: String) {
    let image = XCTAttachment(screenshot: app.screenshot()); image.name = "phone-ask-" + phase
    image.lifetime = .keepAlways; add(image)
    let ax = XCTAttachment(string: app.debugDescription); ax.name = "phone-ask-" + phase + "-AX"
    ax.lifetime = .keepAlways; add(ax)
  }
}
