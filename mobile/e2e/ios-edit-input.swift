import XCTest

// Packaged offline fixture with /?mock=1&tick=0&history=input-images and Local mode.
final class BindFlowTests: XCTestCase {
  func testEditAndResendPairedInput() {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let original = "Screenshots from the PC"
    let edit = app.buttons["Edit message"]
    XCTAssertTrue(edit.waitForExistence(timeout: 8))
    edit.tap()
    let input = app.textViews["Edit message"]
    XCTAssertTrue(input.waitForExistence(timeout: 8))
    XCTAssertEqual(input.value as? String, original)
    record(app, "original")

    input.tap()
    XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 8))
    input.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.25)).tap()
    input.typeText(" - compare them")
    let edited = original + " - compare them"
    XCTAssertEqual(input.value as? String, edited)
    let resend = app.buttons["Resend"]
    XCTAssertTrue(resend.isHittable)
    resend.tap()

    XCTAssertTrue(app.staticTexts[edited].waitForExistence(timeout: 8))
    XCTAssertFalse(app.staticTexts["I received both screenshots."].exists)
    XCTAssertEqual(app.buttons.matching(identifier: "View image").count, 2)
    record(app, "replacement")
    back.tap()
    thread.tap()
    XCTAssertTrue(app.staticTexts[edited].waitForExistence(timeout: 8))
    XCTAssertEqual(app.buttons.matching(identifier: "View image").count, 2)
    record(app, "reopened")
  }

  private func record(_ app: XCUIApplication, _ phase: String) {
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "edit-input-" + phase
    shot.lifetime = .keepAlways
    add(shot)
  }
}
