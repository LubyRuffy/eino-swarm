import XCTest

// Run on iPhone 17 with scripts/ios-layout-fixture.py. The fixture copies the
// packaged app and enables the existing offline walkthrough; shipping assets
// and the user's installed app are never edited.
final class BindFlowTests: XCTestCase {
  func testLeftEdgeSwipeReturnsThroughAgentPages() throws {
    let app = XCUIApplication()
    app.launch()
    let agents = app.buttons["Agents (3)"]
    XCTAssertTrue(agents.waitForExistence(timeout: 20), "requires history=streamed-agents")
    agents.tap()
    let group = app.staticTexts["Active (3)"]
    XCTAssertTrue(group.waitForExistence(timeout: 8))
    let worker = app.buttons.matching(NSPredicate(format: "label CONTAINS 'worker-1'")).firstMatch
    XCTAssertTrue(worker.isHittable)
    worker.tap()
    XCTAssertTrue(app.staticTexts["Worker evidence"].waitForExistence(timeout: 8))
    recordAgentPage(app, name: "edge-back-worker-before")

    leftEdgeBack(app)
    let workerReturned = group.waitForExistence(timeout: 5)
    recordAgentPage(app, name: "edge-back-worker-to-roster")
    XCTAssertTrue(workerReturned, "one native left-edge swipe must return from worker to the Agents list")
    guard workerReturned else { return }
    XCTAssertFalse(app.staticTexts["Worker evidence"].exists)
    assertNoManagerHistory(app)

    leftEdgeBack(app)
    XCTAssertTrue(agents.waitForExistence(timeout: 5), "one swipe from Agents must return to the manager, not skip to the inbox")
    XCTAssertTrue(app.textViews["Message"].exists)
    recordAgentPage(app, name: "edge-back-roster-to-manager")

    leftEdgeBack(app)
    let inboxThread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Trim the layout pass'")).firstMatch
    XCTAssertTrue(inboxThread.waitForExistence(timeout: 5), "the next swipe must return to the inbox")
    XCTAssertFalse(app.buttons["Back"].exists)
    recordAgentPage(app, name: "edge-back-manager-to-inbox")
    leftEdgeBack(app)
    XCTAssertTrue(inboxThread.exists, "a root swipe must leave the inbox open")
    XCTAssertFalse(app.buttons["Back"].exists)
    recordAgentPage(app, name: "edge-back-inbox-root-unchanged")
  }

  func testLeftEdgeSwipeDismissesModelPickerBeforeThread() throws {
    let app = XCUIApplication()
    app.launch()
    let agents = app.buttons["Agents (3)"]
    XCTAssertTrue(agents.waitForExistence(timeout: 20))
    let model = app.buttons["Model"].firstMatch
    XCTAssertTrue(model.waitForExistence(timeout: 8))
    model.tap()
    let close = app.buttons["Close"].firstMatch
    XCTAssertTrue(close.waitForExistence(timeout: 5))
    recordAgentPage(app, name: "edge-back-model-picker-before")
    leftEdgeBack(app, y: 0.8)
    XCTAssertTrue(close.waitForNonExistence(timeout: 5), "the dynamically installed model-picker hook must own the first swipe")
    XCTAssertTrue(agents.exists, "dismissing the model picker must preserve the conversation")
    XCTAssertTrue(app.textViews["Message"].exists)
    recordAgentPage(app, name: "edge-back-model-picker-dismissed")
    leftEdgeBack(app)
    let inboxThread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Trim the layout pass'")).firstMatch
    XCTAssertTrue(inboxThread.waitForExistence(timeout: 5), "the restored conversation hook must own the next swipe")
    recordAgentPage(app, name: "edge-back-after-model-picker-to-inbox")
  }

  func testEdgeSwipeIgnoresShortVerticalAndNonEdgeMotion() throws {
    let app = XCUIApplication()
    app.launch()
    let agents = app.buttons["Agents (3)"]
    XCTAssertTrue(agents.waitForExistence(timeout: 20))
    agents.tap()
    let group = app.staticTexts["Active (3)"]
    XCTAssertTrue(group.waitForExistence(timeout: 8))
    for (name, start, end) in [
      ("short", CGVector(dx: 0.01, dy: 0.45), CGVector(dx: 0.09, dy: 0.45)),
      ("vertical", CGVector(dx: 0.01, dy: 0.4), CGVector(dx: 0.01, dy: 0.7)),
      ("center-right", CGVector(dx: 0.4, dy: 0.6), CGVector(dx: 0.85, dy: 0.6)),
      ("right-edge-left", CGVector(dx: 0.99, dy: 0.6), CGVector(dx: 0.2, dy: 0.6)),
    ] {
      app.coordinate(withNormalizedOffset: start).press(forDuration: 0.05,
        thenDragTo: app.coordinate(withNormalizedOffset: end))
      XCTAssertTrue(group.exists, "\(name) motion must not navigate")
      XCTAssertFalse(agents.exists, "\(name) motion must not leave the Agents page")
      recordAgentPage(app, name: "edge-back-ignored-" + name)
    }
    leftEdgeBack(app)
    XCTAssertTrue(agents.waitForExistence(timeout: 5), "a subsequent committed edge swipe must still work")
    recordAgentPage(app, name: "edge-back-valid-after-ignored-motion")
  }

  private func leftEdgeBack(_ app: XCUIApplication, y: CGFloat = 0.45) {
    let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: y))
    let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.75, dy: y))
    start.press(forDuration: 0.05, thenDragTo: end)
  }

  func testModelResponseErrorIsLocalizedWithoutToolBlame() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons.matching(NSPredicate(format: "label IN %@", ["Back", "返回"])).firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    let chinese = back.label == "返回"
    let expected = chinese ? "模型服务返回的响应无法解析。" : "the model service returned a response that could not be decoded"
    let retry = chinese ? "模型服务返回的响应无法解析，正在重试。" : "The model service returned a response that could not be decoded. Retrying."
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    for phase in ["first-open", "reopened"] {
      thread.tap()
      let error = app.staticTexts[expected]
      XCTAssertTrue(error.waitForExistence(timeout: 8), "requires history=model-response-error; model-response decoding is independent of tool arguments")
      XCTAssertEqual(error.label, expected)
      let retries = app.staticTexts.matching(NSPredicate(format: "label == %@", retry))
      XCTAssertEqual(retries.count, 2, "both bounded model-response retries must remain visible")
      for forbidden in ["tool call", "工具调用", "NodeRunError", "node path:"] {
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS[cd] %@", forbidden)).firstMatch.exists)
      }
      XCTAssertFalse(app.buttons[chinese ? "停止" : "Stop"].exists)
      let send = app.buttons[chinese ? "发送" : "Send"]
      XCTAssertTrue(send.waitForExistence(timeout: 5))
      XCTAssertFalse(send.isEnabled, "the completed failed turn must be idle with an empty draft")
      XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label MATCHES %@", "^[0-9]+ (tools?|个工具)$")).count, 0)
      let screenshot = XCTAttachment(screenshot: app.screenshot())
      screenshot.name = "model-response-error-" + phase
      screenshot.lifetime = .keepAlways
      add(screenshot)
      let nodes = XCTAttachment(string: app.debugDescription)
      nodes.name = "model-response-error-accessibility-" + phase
      nodes.lifetime = .keepAlways
      add(nodes)
      back.tap()
      XCTAssertTrue(thread.waitForExistence(timeout: 8))
    }
  }

  func testModelAPIErrorShowsHTTPStatusAndDecodeCause() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    let expected = "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input"
    for phase in ["first-open", "reopened"] {
      thread.tap()
      let error = app.staticTexts[expected]
      XCTAssertTrue(error.waitForExistence(timeout: 8), "requires history=api-error; retain the actual HTTP status and decode cause")
      XCTAssertEqual(error.label, expected)
      for forbidden in ["a tool call", "NodeRunError", "node path:"] {
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS[cd] %@", forbidden)).firstMatch.exists)
      }
      XCTAssertFalse(app.buttons["Stop"].exists, "a failed completed turn must be idle")
      let send = app.buttons["Send"]
      XCTAssertTrue(send.waitForExistence(timeout: 5))
      XCTAssertFalse(send.isEnabled, "the idle empty draft must not send")
      let screenshot = XCTAttachment(screenshot: app.screenshot())
      screenshot.name = "model-api-error-" + phase
      screenshot.lifetime = .keepAlways
      add(screenshot)
      let nodes = XCTAttachment(string: app.debugDescription)
      nodes.name = "model-api-error-accessibility-" + phase
      nodes.lifetime = .keepAlways
      add(nodes)
      back.tap()
      XCTAssertTrue(thread.waitForExistence(timeout: 8))
    }
  }

  func testAgentPagesExcludeStreamedManagerHistoryAfterEarlier() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    let agents = app.buttons["Agents (3)"]
    XCTAssertTrue(agents.waitForExistence(timeout: 20), "requires the live three-worker agents fixture")
    let manager = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Manager update'"))
    XCTAssertTrue(manager.firstMatch.exists, "the manager must render the live seq-zero updates first")
    XCTAssertGreaterThan(app.buttons.matching(identifier: "1 tool").count, 0, "the manager conversation must contain a real Work fold before switching pages")
    let earlier = app.buttons["Earlier"]
    XCTAssertTrue(earlier.isHittable)
    earlier.tap()
    XCTAssertTrue(earlier.waitForNonExistence(timeout: 8), "Earlier must complete the real older-history path")
    XCTAssertTrue(app.staticTexts["Older manager answer"].exists)
    recordAgentPage(app, name: "agents-manager-after-earlier")
    agents.tap()
    let group = app.staticTexts["Active (3)"]
    XCTAssertTrue(group.waitForExistence(timeout: 8))
    let rows = app.buttons.matching(NSPredicate(format: "label CONTAINS 'worker-'"))
    XCTAssertEqual(rows.count, 3)
    recordAgentPage(app, name: "agents-roster-after-earlier")
    assertNoManagerHistory(app)
    let reader = app.buttons.matching(NSPredicate(format: "label CONTAINS 'worker-1'")).firstMatch
    XCTAssertTrue(reader.isHittable)
    reader.tap()
    XCTAssertTrue(app.staticTexts["Worker evidence"].waitForExistence(timeout: 8))
    recordAgentPage(app, name: "agents-reader-detail")
    XCTAssertFalse(manager.firstMatch.exists, "worker detail must exclude manager paragraphs")
    back.tap()
    XCTAssertTrue(group.waitForExistence(timeout: 8))
    recordAgentPage(app, name: "agents-roster-after-worker-return")
    assertNoManagerHistory(app)
    back.tap()
    XCTAssertTrue(agents.waitForExistence(timeout: 8))
    XCTAssertTrue(manager.firstMatch.exists, "returning to the manager must restore its conversation")
    for index in 0..<3 {
      XCTAssertTrue(app.staticTexts["Manager update \(index)"].exists)
    }
    XCTAssertTrue(app.staticTexts["Older manager answer"].exists)
    XCTAssertFalse(app.staticTexts["Worker evidence"].exists)
    recordAgentPage(app, name: "agents-manager-after-return")
  }

  private func assertNoManagerHistory(_ app: XCUIApplication) {
    let body = app.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] 'manager'"))
    XCTAssertEqual(body.count, 0, "the entire Agents page must exclude manager output, including offscreen accessibility nodes")
    let folds = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Thought' OR label MATCHES '[0-9]+ tools?'"))
    XCTAssertEqual(folds.count, 0, "the Agents roster must not retain the manager's Work fold")
  }

  private func recordAgentPage(_ app: XCUIApplication, name: String) {
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = name
    shot.lifetime = .keepAlways
    add(shot)
    let tree = XCTAttachment(string: app.debugDescription)
    tree.name = name + "-accessibility"
    tree.lifetime = .keepAlways
    add(tree)
  }

  func testWorkerTimeoutShowsPausedErrorInsteadOfRunning() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let error = app.staticTexts.matching(NSPredicate(format: "label CONTAINS '3 consecutive watchdog timeouts'")).firstMatch
    XCTAssertTrue(error.waitForExistence(timeout: 8), "requires history=timeout; the terminal failure must be visible")
    XCTAssertFalse(app.buttons["Stop"].exists)
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "worker-timeout-paused"
    shot.lifetime = .keepAlways
    add(shot)
  }

  func testIndependentTaskMessageHasPeerProvenance() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let message = app.staticTexts["Peer task peer-task: Peer coordination request."]
    XCTAssertTrue(message.waitForExistence(timeout: 8), "requires history=peers; the source must be visible in the packaged app")
    XCTAssertFalse(app.buttons["Stop"].exists, "an inbox message must not start an idle task")
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "peer-task-source"
    shot.lifetime = .keepAlways
    add(shot)
  }

  func testScheduledTurnBoundariesShowCompletionDates() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let first = app.staticTexts["First completed run"]
    XCTAssertTrue(first.waitForExistence(timeout: 8), "scheduled history did not reach the packaged app")
    let second = app.staticTexts["Second completed run"]
    XCTAssertTrue(second.waitForExistence(timeout: 8))
    let dates = app.staticTexts.matching(NSPredicate(format: "label CONTAINS '2026'"))
    XCTAssertGreaterThanOrEqual(dates.count, 2, "both completed turns need a visible date")
    XCTAssertLessThan(first.frame.minY, second.frame.minY)
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "scheduled-turn-boundaries"
    shot.lifetime = .keepAlways
    add(shot)
  }

  func testHiddenHistoryShowsEarlierAnswer() throws {
    let app = XCUIApplication()
    app.launch()
    let explanation = app.staticTexts["No visible messages in this turn yet. Open earlier history."]
    guard explanation.waitForExistence(timeout: 8) else {
      throw XCTSkip("requires IOS_LAYOUT_QUERY=/?mock=1&tick=0&history=hidden")
    }
    let earlier = app.buttons["Earlier"]
    XCTAssertTrue(earlier.isHittable)
    earlier.tap()
    XCTAssertTrue(app.staticTexts["Earlier answer"].waitForExistence(timeout: 8))
    XCTAssertFalse(explanation.exists)
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "hidden-history-earlier-answer"
    shot.lifetime = .keepAlways
    add(shot)
  }

  func testPhoneOutputPreview() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let thread = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Sweep the unused exports'")).firstMatch
    XCTAssertTrue(thread.waitForExistence(timeout: 8))
    thread.tap()
    let outputs = app.buttons["Outputs"]
    XCTAssertTrue(outputs.waitForExistence(timeout: 8))
    outputs.tap()
    let folder = app.buttons["outputs"]
    XCTAssertTrue(folder.waitForExistence(timeout: 8))
    XCTAssertFalse(app.buttons["report.md"].exists)
    folder.tap()
    let file = app.buttons["report.md"]
    XCTAssertTrue(file.waitForExistence(timeout: 8))
    file.tap()
    XCTAssertTrue(app.staticTexts["Verified output"].waitForExistence(timeout: 8))
    XCTAssertTrue(app.staticTexts["A finding"].exists)
    XCTAssertFalse(app.staticTexts["# Verified output"].exists)
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "phone-output-preview"
    shot.lifetime = .keepAlways
    add(shot)
    app.buttons["Back to list"].tap()
    XCTAssertTrue(file.waitForExistence(timeout: 5))
    app.buttons["sample.go"].tap()
    XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'hello'")).firstMatch.waitForExistence(timeout: 8))
    let codeShot = XCTAttachment(screenshot: app.screenshot())
    codeShot.name = "phone-output-code-highlight"
    codeShot.lifetime = .keepAlways
    add(codeShot)
  }

  func testClientTaskHeaderStaysVisibleAfterPull() throws {
    let app = XCUIApplication()
    app.launch()
    let back = app.buttons["Back"].firstMatch
    XCTAssertTrue(back.waitForExistence(timeout: 12))
    back.tap()
    let task = app.buttons["open session"]
    XCTAssertTrue(task.waitForExistence(timeout: 8))
    // The task starts near the inbox footer on this fixture. Bring it clear of
    // the bottom action bar so XCTest taps the row rather than the overlay.
    app.swipeUp()
    XCTAssertTrue(task.isHittable)
    task.tap()
    XCTAssertTrue(back.waitForExistence(timeout: 8))
    let headerY = back.frame.minY
    let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
    let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.65))
    start.press(forDuration: 0.15, thenDragTo: end)
    let shot = XCTAttachment(screenshot: app.screenshot())
    shot.name = "client-task-header-after-pull"
    shot.lifetime = .keepAlways
    add(shot)
    XCTAssertTrue(back.isHittable, "client task Back must remain above the inbox")
    XCTAssertEqual(back.frame.minY, headerY, accuracy: 2, "inbox pull moved the client task header")
    back.tap()
    XCTAssertTrue(task.waitForExistence(timeout: 5), "Back must return to the inbox")
  }

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

  func testComposerStopsLiveTurn() throws {
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
    let stops = app.buttons.matching(NSPredicate(format: "label == 'Stop'"))
    XCTAssertTrue(stops.firstMatch.waitForExistence(timeout: 5))
    let controls = stops.allElementsBoundByIndex
    XCTAssertEqual(controls.count, 2, "the header Stop alone does not accept the composer control")
    let stop = try XCTUnwrap(controls.max { $0.frame.midY < $1.frame.midY })
    XCTAssertGreaterThan(stop.frame.minY, app.frame.midY, "Stop must be the bottom composer control")
    XCTAssertEqual(stop.frame.width, stop.frame.height, accuracy: 1, "the primary control must remain an icon button")
    XCTAssertTrue(stop.isEnabled)
    XCTAssertTrue(stop.isHittable)
    let before = XCTAttachment(screenshot: app.screenshot())
    before.name = "composer-stop-square-before"
    before.lifetime = .keepAlways
    add(before)
    stop.tap()
    XCTAssertTrue(stops.firstMatch.waitForNonExistence(timeout: 8), "stopping must clear both running controls")
    let send = app.buttons["Send"]
    XCTAssertTrue(send.waitForExistence(timeout: 5), "the idle composer must return to Send")
    XCTAssertFalse(send.isEnabled, "stopping an empty draft must not send a message")
    XCTAssertTrue(message.exists)
    let value = try XCTUnwrap(message.value as? String)
    XCTAssertTrue(["", "Message"].contains(value))
    let after = XCTAttachment(screenshot: app.screenshot())
    after.name = "composer-stop-idle-send-after"
    after.lifetime = .keepAlways
    add(after)
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
    message.tap()
    message.typeText("long input " + String(repeating: "words ", count: 15))
    let submit = app.buttons["Follow-up"]
    XCTAssertTrue(submit.waitForExistence(timeout: 5))
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
