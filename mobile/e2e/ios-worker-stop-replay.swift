import XCTest

// Runs only in an isolated packaged history=stopped-workers fixture. Returning
// through Home keeps the same MockHost and reopens its stored watch history.
final class BindFlowTests: XCTestCase {
  func testNativeWorkerStopReplayRetainsStoppedAndFinishedOutcomes() throws {
    continueAfterFailure = false
    print("Issue85 native packaged-app Stop and stored-replay driver")
    let app = XCUIApplication()
    app.launch()
    XCTAssertTrue(app.buttons["Stop"].firstMatch.waitForExistence(timeout: 20))
    XCTAssertTrue(app.staticTexts["Trim the layout pass"].exists)
    XCTAssertTrue(app.buttons["Agents (2)"].exists)
    app.buttons["Agents (2)"].tap()
    XCTAssertTrue(heading(app, "Active (2)").waitForExistence(timeout: 5))
    assertWorker(app, "reader-1", "running")
    assertWorker(app, "writer-2", "running")
    assertWorker(app, "done-3", "done")
    assertWorker(app, "failed-4", "failed")
    record(app, "worker-stop-live-roster")
    app.buttons["Back"].tap()
    app.buttons["Stop"].firstMatch.tap()
    XCTAssertTrue(app.buttons["Agents (0)"].waitForExistence(timeout: 8))

    for replay in 0...1 {
      app.buttons["Agents (0)"].tap()
      XCTAssertTrue(heading(app, "Stopped (2)").waitForExistence(timeout: 8))
      XCTAssertTrue(heading(app, "Finished (2)").exists)
      XCTAssertFalse(heading(app, "Active (2)").exists)
      assertWorker(app, "reader-1", "stopped")
      assertWorker(app, "writer-2", "stopped")
      assertWorker(app, "done-3", "done")
      assertWorker(app, "failed-4", "failed")
      record(app, "worker-stop-roster-\(replay)")

      worker(app, "reader-1").tap()
      XCTAssertTrue(text(app, "swarm: agent reader-1 cancelled: context canceled")
        .waitForExistence(timeout: 8))
      XCTAssertFalse(app.buttons["Stop"].exists)
      record(app, "worker-stop-cancelled-detail-\(replay)")
      app.buttons["Back"].tap()
      if replay == 0 {
        worker(app, "done-3").tap()
        // A successful finish is a roster outcome, not a worker answer block.
        XCTAssertTrue(app.staticTexts["worker"].waitForExistence(timeout: 5))
        XCTAssertFalse(text(app, "provider unavailable").exists)
        XCTAssertFalse(text(app, "context canceled").exists)
        XCTAssertFalse(app.buttons["Stop"].exists)
        record(app, "worker-stop-success-detail")
        app.buttons["Back"].tap()
        worker(app, "failed-4").tap()
        XCTAssertTrue(text(app, "provider unavailable").waitForExistence(timeout: 5))
        record(app, "worker-stop-failure-detail")
        app.buttons["Back"].tap()
        app.buttons["Back"].tap()
        app.buttons["Back"].tap()
        let sameThread = app.buttons["Open Trim the layout pass"]
        XCTAssertTrue(sameThread.waitForExistence(timeout: 8))
        record(app, "worker-stop-same-host-Home")
        sameThread.tap()
        XCTAssertTrue(app.buttons["Agents (0)"].waitForExistence(timeout: 8))
        XCTAssertFalse(app.buttons["Stop"].exists)
      }
    }
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 {
      record(XCUIApplication(), "worker-stop-native-failure")
    }
  }

  private func worker(_ app: XCUIApplication, _ id: String) -> XCUIElement {
    app.buttons.matching(NSPredicate(format: "label CONTAINS %@", id)).firstMatch
  }

  private func assertWorker(_ app: XCUIApplication, _ id: String, _ status: String) {
    let matches = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", id))
    XCTAssertEqual(matches.count, 1, "Each original worker identity occurs once")
    XCTAssertTrue(matches.firstMatch.label.lowercased().hasSuffix(status))
  }

  private func heading(_ app: XCUIApplication, _ label: String) -> XCUIElement {
    app.staticTexts.matching(NSPredicate(format: "label ==[c] %@", label)).firstMatch
  }

  private func text(_ app: XCUIApplication, _ value: String) -> XCUIElement {
    app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", value)).firstMatch
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
