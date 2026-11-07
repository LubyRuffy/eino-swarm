import XCTest
import Vision

// The private ios-layout fixture selects the same EPUB used by the browser E2E.
final class BindFlowTests: XCTestCase {
  func testPhoneEpubUsesReadingSpaceAndTapNavigation() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 30))
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
    let download = app.buttons["Download"]
    let contents = app.descendants(matching: .any).matching(identifier: "Contents").firstMatch
    func tapPage(_ x: CGFloat) {
      web.coordinate(withNormalizedOffset: CGVector(dx: x, dy: 0.56)).tap()
    }
    // Some WebKit runtimes expose the sandboxed frame without its text in AX.
    // Read chapter text from the actual painted page and keep app controls in AX.
    func readingText(_ text: String, prefix: Bool = false) -> CGRect? {
      let screenshot = app.screenshot().image
      guard let image = screenshot.cgImage else { return nil }
      let request = VNRecognizeTextRequest()
      request.recognitionLevel = .fast
      request.usesLanguageCorrection = false
      do {
        try VNImageRequestHandler(cgImage: image).perform([request])
      } catch {
        XCTFail("Reading screenshot OCR failed: \(error)")
        return nil
      }
      let result = request.results?.first { observation in
        guard let label = observation.topCandidates(1).first?.string else { return false }
        return prefix ? label.hasPrefix(text) : label == text
      }
      guard let frame = result?.boundingBox else { return nil }
      return CGRect(x: frame.minX * web.frame.width, y: frame.minY * web.frame.height,
        width: frame.width * web.frame.width, height: frame.height * web.frame.height)
    }
    func waitFor(_ predicate: @escaping () -> Bool, _ message: String) {
      let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in predicate() }, object: nil)
      XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 5), .completed, message)
    }
    waitFor({ readingText("The arrival") != nil }, "the first chapter should be visible in the packaged WebView")
    record(app, "epub-reading-default")
    XCTAssertFalse(download.exists, "the ready reader starts without a persistent download row")
    XCTAssertFalse(contents.exists)
    XCTAssertEqual(app.staticTexts.matching(identifier: filename).count, 1)
    XCTAssertFalse(app.staticTexts["outputs/\(filename)"].exists)
    XCTAssertFalse(app.buttons["Previous"].exists)
    XCTAssertFalse(app.buttons["Next"].exists)

    let firstHeadingFrame = try XCTUnwrap(readingText("The arrival"))
    XCTAssertNil(readingText("Passage 9.", prefix: true), "the later passage starts outside the first page")
    tapPage(0.12)
    XCTAssertEqual(readingText("The arrival"), firstHeadingFrame, "left at the beginning must keep the first page")
    tapPage(0.88)
    waitFor({ readingText("Passage 9.", prefix: true) != nil }, "the turned page must show later text from the same chapter")
    XCTAssertNil(readingText("The garden"), "the first turn stays within the first chapter")
    record(app, "epub-reading-next-page")
    tapPage(0.12)
    waitFor({ readingText("The arrival") != nil }, "left should return to the first page")
    XCTAssertNil(readingText("Passage 9.", prefix: true))
    XCTAssertEqual(readingText("The arrival"), firstHeadingFrame)

    tapPage(0.5)
    XCTAssertTrue(contents.waitForExistence(timeout: 5) && contents.isHittable)
    XCTAssertTrue(download.isHittable)
    record(app, "epub-reading-controls")
    tapPage(0.5)
    XCTAssertTrue(contents.waitForNonExistence(timeout: 5))
    XCTAssertFalse(download.exists)
    XCTAssertEqual(readingText("The arrival"), firstHeadingFrame, "showing controls must preserve the page position")

    for _ in 0..<12 {
      if readingText("The garden") != nil { break }
      tapPage(0.88)
    }
    waitFor({ readingText("The garden") != nil }, "right turns should cross the chapter boundary")
    tapPage(0.12)
    waitFor({ readingText("Passage 35.", prefix: true) != nil }, "left from the next chapter should return to the previous chapter’s final page")
    record(app, "epub-reading-previous-chapter-end")

    tapPage(0.88)
    waitFor({ readingText("The garden") != nil }, "the short chapter should remain reachable before pinching")
    let navigation = app.otherElements["EPUB navigation"].firstMatch
    XCTAssertTrue(navigation.exists && navigation.frame.height > web.frame.height / 2)
    let title = app.staticTexts[filename].firstMatch
    let titleFrame = title.frame
    let chapterFrame = try XCTUnwrap(readingText("The garden"))
    let increase = app.buttons["Increase text size"]
    let decrease = app.buttons["Decrease text size"]
    let reset = app.buttons["Reset text size"]

    navigation.pinch(withScale: 1.6, velocity: 1)
    waitFor({ (readingText("The garden")?.height ?? 0) > chapterFrame.height + 2 },
      "spreading two fingers should enlarge the text and keep the chapter visible")
    XCTAssertEqual(title.frame, titleFrame, "a reading pinch must not zoom the app header")
    XCTAssertFalse(contents.exists || download.exists || increase.exists, "a pinch must not reveal center controls")
    record(app, "epub-reading-pinch-out")

    let enlargedHeight = try XCTUnwrap(readingText("The garden")).height
    navigation.pinch(withScale: 0.7, velocity: -1)
    waitFor({ readingText("The garden").map { $0.height < enlargedHeight - 2 } == true },
      "bringing two fingers together should shrink text on the current chapter")
    XCTAssertEqual(title.frame, titleFrame)
    XCTAssertFalse(contents.exists || download.exists || decrease.exists)
    record(app, "epub-reading-pinch-in")

    tapPage(0.5)
    XCTAssertTrue(contents.waitForExistence(timeout: 5) && contents.isHittable)
    XCTAssertTrue(increase.isHittable && decrease.isHittable && reset.isHittable)
    if reset.isEnabled { reset.tap() }
    XCTAssertTrue(app.staticTexts["18 px"].exists)
    record(app, "epub-reading-pinch-controls")
    tapPage(0.5)
    XCTAssertTrue(contents.waitForNonExistence(timeout: 5))
    waitFor({ readingText("The garden").map { abs($0.height - chapterFrame.height) < 1 } == true },
      "reset should restore the original reading size and keep the current chapter")
    record(app, "epub-reading-pinch-reset")
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
