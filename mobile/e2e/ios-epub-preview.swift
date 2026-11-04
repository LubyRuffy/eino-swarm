import XCTest

// The private ios-layout fixture selects the same EPUB used by the browser E2E.
final class BindFlowTests: XCTestCase {
  func testPhoneEpubUsesReadingSpaceAndTapNavigation() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 15))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let outputs = app.buttons["Outputs"]
    XCTAssertTrue(outputs.waitForExistence(timeout: 8))
    outputs.tap()
    let folder = app.buttons["outputs"]
    XCTAssertTrue(folder.waitForExistence(timeout: 8))
    folder.tap()
    let filename = "reading-test.epub"
    let file = app.buttons[filename]
    XCTAssertTrue(file.waitForExistence(timeout: 8), "requires the file-preview=epub fixture")
    file.tap()

    let web = app.webViews.firstMatch
    let arrival = app.staticTexts["The arrival"].firstMatch
    let garden = app.staticTexts["The garden"].firstMatch
    let secondPage = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Passage 9.")).firstMatch
    let download = app.buttons["Download"]
    let contents = app.descendants(matching: .any).matching(identifier: "Contents").firstMatch
    func tapPage(_ x: CGFloat) {
      web.coordinate(withNormalizedOffset: CGVector(dx: x, dy: 0.56)).tap()
    }
    func isOnScreen(_ element: XCUIElement) -> Bool {
      guard element.exists else { return false }
      let frame = element.frame
      return !frame.isEmpty && web.frame.contains(CGPoint(x: frame.midX, y: frame.midY))
    }
    func waitFor(_ predicate: @escaping () -> Bool, _ message: String) {
      let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in predicate() }, object: nil)
      XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 5), .completed, message)
    }
    waitFor({ isOnScreen(arrival) }, "the first chapter should be visible in the packaged WebView")
    record(app, "epub-reading-default")
    XCTAssertFalse(download.exists, "the ready reader starts without a persistent download row")
    XCTAssertFalse(contents.exists)
    XCTAssertEqual(app.staticTexts.matching(identifier: filename).count, 1)
    XCTAssertFalse(app.staticTexts["outputs/\(filename)"].exists)
    XCTAssertFalse(app.buttons["Previous"].exists)
    XCTAssertFalse(app.buttons["Next"].exists)

    let firstHeadingFrame = arrival.frame
    XCTAssertFalse(isOnScreen(secondPage), "the later passage starts outside the first page")
    tapPage(0.12)
    XCTAssertEqual(arrival.frame, firstHeadingFrame, "left at the beginning must keep the first page")
    tapPage(0.88)
    // iOS clips negative accessibility coordinates to zero. Observe later
    // text moving onto the screen instead of testing the off-left heading.
    waitFor({ isOnScreen(secondPage) }, "the turned page must show later text from the same chapter")
    XCTAssertFalse(isOnScreen(garden), "the first turn stays within the first chapter")
    record(app, "epub-reading-next-page")
    tapPage(0.12)
    waitFor({ isOnScreen(arrival) && !isOnScreen(secondPage) }, "left should return to the first page")
    XCTAssertEqual(arrival.frame, firstHeadingFrame)

    tapPage(0.5)
    XCTAssertTrue(contents.waitForExistence(timeout: 5) && contents.isHittable)
    XCTAssertTrue(download.isHittable)
    XCTAssertEqual(arrival.frame, firstHeadingFrame, "showing controls must preserve the page position")
    record(app, "epub-reading-controls")
    tapPage(0.5)
    XCTAssertTrue(contents.waitForNonExistence(timeout: 5))
    XCTAssertFalse(download.exists)

    for _ in 0..<12 {
      if isOnScreen(garden) { break }
      tapPage(0.88)
    }
    waitFor({ isOnScreen(garden) }, "right turns should cross the chapter boundary")
    let lastPassage = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Passage 35.")).firstMatch
    tapPage(0.12)
    waitFor({ isOnScreen(lastPassage) }, "left from the next chapter should return to the previous chapter’s final page")
    record(app, "epub-reading-previous-chapter-end")
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "epub-native-failure")
    }
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
