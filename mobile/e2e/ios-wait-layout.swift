import XCTest

// Run on iPhone 17 with scripts/ios-layout-fixture.py. The fixture copies the
// packaged app and enables the existing offline walkthrough; shipping assets
// and the user's installed app are never edited.
final class BindFlowTests: XCTestCase {
  func testQuoteActionBesideNativeSelectionMenu() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"]
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let answer = app.staticTexts["Here is what changed:"].firstMatch
    XCTAssertTrue(answer.waitForExistence(timeout: 8))
    answer.press(forDuration: 1.2)
    let action = app.buttons["Add to chat"]
    XCTAssertTrue(action.waitForExistence(timeout: 5), "the selected answer must expose Add to chat")
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "selected-answer-native-menu"
    shot.lifetime = .keepAlways
    add(shot)
    XCTAssertTrue(action.isHittable, "the native selection menu must not cover Add to chat")
    action.tap()
    XCTAssertTrue(app.textViews["Edit quote 1"].firstMatch.waitForExistence(timeout: 5),
                  "tapping Add to chat must create a quote")
  }

  func testComposerSubmissionAfterTyping() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 10))
    back.tap()
    let live = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Trim the layout pass'")).firstMatch
    XCTAssertTrue(live.waitForExistence(timeout: 5))
    live.tap()
    let message = app.textViews["Message"]
    XCTAssertTrue(message.waitForExistence(timeout: 10))
    let submit = app.buttons["Follow-up"]
    XCTAssertTrue(submit.waitForExistence(timeout: 5))
    message.tap()
    message.typeText("long input " + String(repeating: "words ", count: 15))
    let done = app.toolbars.buttons.allElementsBoundByIndex.last
    if let done, done.exists {
      done.tap()
    } else {
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.55)).tap()
    }
    XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 3))
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.lifetime = .keepAlways
    add(shot)
    XCTAssertGreaterThan(submit.frame.width, 0)
    XCTAssertLessThanOrEqual(submit.frame.maxX, app.frame.maxX, "submission clips outside the iPhone")
    XCTAssertTrue(submit.isHittable, "submission requires horizontal scrolling")
    submit.tap()
    XCTAssertTrue(message.exists)
    let value = try XCTUnwrap(message.value as? String)
    XCTAssertTrue(["", "Message"].contains(value), "the visible submission button must send the draft")
  }

  func testWaitLayoutAfterTyping() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 10))
    back.tap()
    let wait = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Watch the nightly export'")).firstMatch
    XCTAssertTrue(wait.waitForExistence(timeout: 5))
    wait.tap()
    let message = app.textViews["Message"]
    XCTAssertTrue(message.waitForExistence(timeout: 5))
    let headerY = back.frame.minY
    message.tap()
    message.typeText("layout")
    let done = app.toolbars.buttons.allElementsBoundByIndex.last
    if let done, done.exists {
      done.tap()
    } else {
      // iOS 26's WebView accessory checkmark has no accessible label. This
      // coordinate is the observed checkmark on the documented iPhone 17.
      app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.55)).tap()
    }
    XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 3), "keyboard was not dismissed")
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.lifetime = .keepAlways
    add(shot)
    let run = app.buttons["Run now"]
    XCTAssertTrue(run.waitForExistence(timeout: 3))
    XCTAssertLessThanOrEqual(run.frame.maxX, app.frame.maxX, "Run now clips outside the iPhone")
    XCTAssertTrue(back.isHittable, "Back header moved under the status bar")
    XCTAssertEqual(back.frame.minY, headerY, accuracy: 1, "typing left the header under the status bar")
  }
}
