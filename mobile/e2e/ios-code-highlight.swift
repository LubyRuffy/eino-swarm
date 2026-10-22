import XCTest
import UIKit

// Copy into a private packaged app with the existing mock=1&tick=0 entry.
// Paste/send/copy use native controls; no script changes application state.
final class BindFlowTests: XCTestCase {
  func testNativeCppFenceRetainsSourceAndCopyAcrossThemes() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    let source = "const char* title = \"<tag>\"; // note\nreturn 0;"
    let fence = "```cpp\n" + source + "\n```"
    XCUIDevice.shared.appearance = .light
    defer { XCUIDevice.shared.appearance = .light }
    app.launch()
    XCTAssertTrue(app.buttons["Back"].firstMatch.waitForExistence(timeout: 20))
    app.buttons["Back"].firstMatch.tap()
    let completed = app.buttons["Open Sweep the unused exports"].firstMatch
    XCTAssertTrue(completed.waitForExistence(timeout: 8))
    completed.tap()
    let message = app.textViews["Message"].firstMatch
    XCTAssertTrue(message.waitForExistence(timeout: 8))
    UIPasteboard.general.string = fence
    message.tap()
    message.press(forDuration: 1.2)
    let paste = app.menuItems["Paste"].firstMatch
    XCTAssertTrue(paste.waitForExistence(timeout: 5))
    paste.tap()
    let allowPaste = app.buttons["Allow Paste"].firstMatch
    if allowPaste.waitForExistence(timeout: 1) { allowPaste.tap() }
    XCTAssertEqual(message.value as? String, fence)
    app.buttons["Send"].firstMatch.tap()
    XCTAssertTrue(app.buttons["Copy code"].firstMatch.waitForExistence(timeout: 8))
    if app.keyboards.firstMatch.exists {
      let accessory = app.toolbars.buttons.allElementsBoundByIndex.last
      if let accessory, accessory.isHittable { accessory.tap() }
    }
    // The generic mock answer follows the message; scroll back to its fence.
    let copy = app.buttons["Copy code"].firstMatch
    if !copy.isHittable { app.webViews.firstMatch.swipeDown() }
    XCTAssertTrue(copy.isHittable)
    XCTAssertTrue(app.staticTexts["cpp"].firstMatch.exists)
    record(app, "cpp-fence-light-before-copy")
    copy.tap()
    // The outer collector must compare `simctl pbpaste` on this owned UDID
    // against source after the case, before shutdown. Runner reads can block.
    record(app, "cpp-fence-light-copied")
    XCUIDevice.shared.appearance = .dark
    // Wait on system appearance, then capture the same reached code block.
    XCTAssertEqual(XCUIDevice.shared.appearance, .dark)
    RunLoop.current.run(until: Date().addingTimeInterval(1))
    XCTAssertTrue(app.staticTexts["cpp"].firstMatch.exists)
    record(app, "cpp-fence-dark")
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "cpp-fence-native-failure")
    }
    XCUIApplication().terminate()
  }

  private func record(_ app: XCUIApplication, _ name: String) {
    let image = XCTAttachment(screenshot: app.screenshot())
    image.name = name
    image.lifetime = .keepAlways
    add(image)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = name + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
  }
}
