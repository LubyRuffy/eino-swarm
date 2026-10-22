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
    let copy = try enterCppFence(app, fence: fence)
    record(app, "cpp-fence-light-before-copy")
    copy.tap()
    // The outer collector must compare `simctl pbpaste` on this owned UDID
    // against source after the case, before shutdown. Runner reads can block.
    record(app, "cpp-fence-light-copied")
    XCUIDevice.shared.appearance = .dark
    // This getter records the request; only the captured frame proves appearance.
    XCTAssertEqual(XCUIDevice.shared.appearance, .dark)
    RunLoop.current.run(until: Date().addingTimeInterval(1))
    XCTAssertTrue(app.staticTexts["cpp"].firstMatch.exists)
    let dark = record(app, "cpp-fence-dark")
    XCTAssertTrue(hasVisibleDarkAppearance(dark.image), "The captured C++ frame must actually be dark and nonblank")
  }

  // The caller establishes system dark before launch; this case never switches themes.
  func testNativeCppFenceRetainsSourceAndCopyInEstablishedDark() throws {
    continueAfterFailure = false
    XCUIDevice.shared.appearance = .dark
    let app = XCUIApplication()
    let source = "const char* title = \"<tag>\"; // note\nreturn 0;"
    let copy = try enterCppFence(app, fence: "```cpp\n" + source + "\n```")
    let before = record(app, "cpp-fence-established-dark-before-copy")
    XCTAssertTrue(hasVisibleDarkAppearance(before.image), "The captured C++ frame must actually be dark and nonblank")
    copy.tap()
    let copied = record(app, "cpp-fence-established-dark-copied")
    XCTAssertTrue(hasVisibleDarkAppearance(copied.image), "Copy must retain the actual dark C++ frame")
  }

  private func enterCppFence(_ app: XCUIApplication, fence: String) throws -> XCUIElement {
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
    return copy
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "cpp-fence-native-failure")
    }
    XCUIApplication().terminate()
  }

  @discardableResult
  private func record(_ app: XCUIApplication, _ name: String) -> XCUIScreenshot {
    let screenshot = app.screenshot()
    let image = XCTAttachment(screenshot: screenshot)
    image.name = name
    image.lifetime = .keepAlways
    add(image)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = name + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
    return screenshot
  }
}

final class DarkScreenshotTests: XCTestCase {
  func testDarkScreenshotRejectsLightContent() {
    XCTAssertFalse(hasVisibleDarkAppearance(themeImage(background: .white, foreground: .black)))
  }

  func testDarkScreenshotAcceptsDarkContent() {
    XCTAssertTrue(hasVisibleDarkAppearance(themeImage(background: .black, foreground: .white)))
  }

  func testDarkScreenshotRejectsBlackFrame() {
    XCTAssertFalse(hasVisibleDarkAppearance(themeImage(background: .black)))
  }

  func testDarkScreenshotRejectsTransparentFrame() {
    XCTAssertFalse(hasVisibleDarkAppearance(themeImage(background: .clear)))
  }

  private func themeImage(background: UIColor, foreground: UIColor? = nil) -> UIImage {
    let format = UIGraphicsImageRendererFormat()
    format.scale = 1
    return UIGraphicsImageRenderer(size: CGSize(width: 160, height: 160), format: format).image { context in
      background.setFill()
      context.fill(CGRect(x: 0, y: 0, width: 160, height: 160))
      if let foreground {
        ("C++\nconst x = 7" as NSString).draw(at: CGPoint(x: 8, y: 24), withAttributes: [
          .font: UIFont.systemFont(ofSize: 28), .foregroundColor: foreground,
        ])
      }
    }
  }

}

// Coarse appearance/blank-frame guard, not a syntax-colour or semantic UI check.
private func hasVisibleDarkAppearance(_ image: UIImage) -> Bool {
  guard let source = image.cgImage,
        let space = CGColorSpace(name: CGColorSpace.sRGB) else { return false }
  let size = 64
  var pixels = [UInt8](repeating: 0, count: size * size * 4)
  let drawn = pixels.withUnsafeMutableBytes { bytes -> Bool in
    guard let context = CGContext(data: bytes.baseAddress, width: size, height: size,
      bitsPerComponent: 8, bytesPerRow: size * 4, space: space,
      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue | CGBitmapInfo.byteOrder32Big.rawValue)
      else { return false }
    context.draw(source, in: CGRect(x: 0, y: 0, width: CGFloat(size), height: CGFloat(size)))
    return true
  }
  guard drawn else { return false }
  var luminance: [Double] = []
  var opaque = 0
  for offset in stride(from: 0, to: pixels.count, by: 4) {
    if pixels[offset + 3] >= 250 { opaque += 1 }
    luminance.append((0.2126 * Double(pixels[offset]) + 0.7152 * Double(pixels[offset + 1])
      + 0.0722 * Double(pixels[offset + 2])) / 255)
  }
  luminance.sort()
  let count = luminance.count
  // Median rejects a light canvas; the percentile span also rejects a blank black frame.
  return Double(opaque) / Double(count) >= 0.98 && luminance[count / 2] < 0.35
    && luminance[count * 99 / 100] - luminance[count / 100] > 0.20
}
