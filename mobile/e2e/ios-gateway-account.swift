import XCTest

// Selected by ios-layout-fixture.py in a fresh, private simulator app. The
// packaged web files are unchanged; only the QA native wrapper selects this
// shared loopback fixture. This does not accept production NOSEC or a signed IPA.
final class BindFlowTests: XCTestCase {
  private let fixtureOrigin = "__ZWAI_GATEWAY_FIXTURE_ORIGIN__"

  func testNativeAccountReviewModelAndOwnedPCFlow() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    try control("review=pending&pc=hide&api=on&start=normal&catalog=normal&model=normal")
    app.launch()

    let login = app.buttons["Sign in with NOSEC"]
    XCTAssertTrue(login.waitForExistence(timeout: 15))
    XCTAssertFalse(app.textFields["Server address"].exists)
    XCTAssertFalse(app.textFields["Hub URL"].exists)
    XCTAssertFalse(app.textFields["API key"].exists)
    record(app, "account-default-no-configuration")

    login.tap()
    XCTAssertTrue(app.staticTexts["User center"].waitForExistence(timeout: 5))

    // These requests go through the shipping WKWebView fetch bridge. Neither
    // an error message alone nor a source mock proves redirect/abort behavior.
    try control("start=redirect")
    app.buttons["Sign in with NOSEC"].tap()
    try waitForCount("redirect_sent", atLeast: 1, timeout: 8)
    XCTAssertTrue(app.staticTexts["The endpoint rejected the request."].waitForExistence(timeout: 8))
    XCTAssertEqual(try count("redirect_target"), 0)
    XCTAssertEqual(try count("browser_start"), 0)
    record(app, "account-native-control-redirect-refused")

    try control("start=delay")
    let delayedAt = Date()
    app.buttons["Sign in with NOSEC"].tap()
    try waitForCount("delay_started", atLeast: 1, timeout: 5)
    try waitForCount("delay_cancelled", atLeast: 1, timeout: 18)
    let elapsed = Date().timeIntervalSince(delayedAt)
    XCTAssertGreaterThanOrEqual(elapsed, 14, "The request must exercise the 15-second control deadline")
    XCTAssertLessThan(elapsed, 19.5, "The native request must abort before the fixture's 20-second late response")
    XCTAssertEqual(try count("delay_completed"), 0)
    XCTAssertEqual(try count("browser_start"), 0)
    XCTAssertTrue(app.staticTexts["The account operation timed out. Try again."].waitForExistence(timeout: 3))
    try waitForEnabled(app.buttons["Sign in with NOSEC"])
    let timing = XCTAttachment(string: "control_request_cancelled_after_seconds=\(elapsed)")
    timing.name = "account-native-control-timeout-timing"
    timing.lifetime = .keepAlways
    add(timing)
    record(app, "account-native-control-timeout-cancelled")

    try control("start=normal")
    app.buttons["Sign in with NOSEC"].tap()
    try browserLogin(app, authorize: false)
    XCTAssertTrue(app.staticTexts["Waiting for administrator approval"].waitForExistence(timeout: 15))
    try assertReviewOnly(app)
    let pending = try counts()
    for key in ["bootstrap", "models", "model_calls", "connect"] {
      XCTAssertEqual(pending[key, default: 0], 0, "No \(key) authority before approval")
    }
    record(app, "account-pending-isolation")

    try control("review=rejected")
    app.buttons["Refresh account"].tap()
    XCTAssertTrue(app.staticTexts["Your account was rejected. Contact an administrator."].waitForExistence(timeout: 15))
    try assertReviewOnly(app)
    record(app, "account-rejected-isolation")

    // A rejected device flow never becomes a credential. Start a new explicit
    // sign-in after the fixture administrator changes the account approval.
    app.buttons["Sign out"].tap()
    try control("review=approved&pc=hide&catalog=redirect")
    XCTAssertTrue(login.waitForExistence(timeout: 8))
    login.tap()
    let userCenter = app.staticTexts["User center"]
    XCTAssertTrue(userCenter.waitForExistence(timeout: 5))
    if app.buttons["Sign in with NOSEC"].exists { app.buttons["Sign in with NOSEC"].tap() }
    try browserLogin(app, authorize: true)

    XCTAssertTrue(app.staticTexts["Could not load server models. Refresh to try again."].waitForExistence(timeout: 20))
    XCTAssertGreaterThan(try count("catalog_redirect_sent"), 0)
    XCTAssertEqual(try count("redirect_target"), 0, "Catalog requests must not forward the account bearer to a redirected origin")
    XCTAssertEqual(try count("connect"), 0)
    record(app, "account-native-catalog-redirect-refused")
    try control("catalog=normal")
    app.buttons["Refresh account"].tap()
    XCTAssertTrue(app.staticTexts["native-fixture-model"].waitForExistence(timeout: 15))
    app.buttons["Chat history"].tap()

    let message = app.textViews["Message"]
    XCTAssertTrue(message.waitForExistence(timeout: 20), "Approved account must reach phone model chat without a PC")
    try control("model=redirect")
    try send(app, "native account redirect probe")
    try waitForCount("model_redirect_sent", atLeast: 1, timeout: 10)
    let status = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "307")).firstMatch
    XCTAssertTrue(status.waitForExistence(timeout: 10), "The refused native model response must retain its HTTP status")
    try waitForEnabled(message)
    XCTAssertEqual(try count("redirect_target"), 0)
    XCTAssertEqual(try count("model_successes"), 0)
    record(app, "account-native-model-redirect-refused")

    try control("model=stall")
    try send(app, "native account cancellation probe")
    try waitForCount("model_stall_started", atLeast: 1, timeout: 10)
    let stop = app.buttons["Stop"].firstMatch
    XCTAssertTrue(stop.waitForExistence(timeout: 5))
    XCTAssertTrue(stop.isHittable)
    stop.tap()
    try waitForCount("model_cancelled", atLeast: 1, timeout: 8)
    XCTAssertEqual(try count("model_completed"), 0, "Stop must disconnect the real native HTTP stream before a late response")
    XCTAssertEqual(try count("model_successes"), 0)
    try waitForEnabled(message)
    record(app, "account-native-model-stop-cancelled")

    try control("model=normal")
    try send(app, "native account model probe")
    XCTAssertTrue(app.staticTexts["Native account answer"].waitForExistence(timeout: 20))
    XCTAssertEqual(try count("model_successes"), 1)
    XCTAssertEqual(try count("connect"), 0)
    record(app, "account-direct-model-without-pc")

    app.buttons["Back"].tap()
    app.buttons["Menu"].tap()
    // WKWebView exposes the account menu action as a native Button.
    let center = app.buttons["User center"]
    XCTAssertTrue(center.waitForExistence(timeout: 5))
    XCTAssertTrue(center.isHittable)
    center.tap()
    try control("pc=show&api=off")
    app.buttons["Refresh account"].tap()
    let connect = app.buttons["Connect Native PC"]
    XCTAssertTrue(connect.waitForExistence(timeout: 12))
    XCTAssertTrue(app.staticTexts["API access is unavailable. Contact an administrator."].exists)
    XCTAssertTrue(connect.isEnabled, "Device authority is independent of model API authorization")
    record(app, "account-owned-pc-picker")
    connect.tap()
    let seed = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "Native PC conversation")).firstMatch
    XCTAssertTrue(seed.waitForExistence(timeout: 20), "The selected PC must answer over the real encrypted Pairlink/RPC path")
    let connected = try counts()
    XCTAssertEqual(connected["connect", default: 0], 1)
    XCTAssertGreaterThan(connected["peer_messages", default: 0], 0)
    for key in ["anonymous_redeem", "anonymous_authorization_leak", "native_cookie_leak", "unauthorized_business", "unapproved_business", "invalid_pkce", "redirect_target", "delay_completed", "model_completed"] {
      XCTAssertEqual(connected[key, default: 0], 0, "Unexpected fixture outcome: \(key)")
    }
    record(app, "account-selected-pc-encrypted-rpc")
  }

  // A complete first callback followed by a short callback used to trap inside
  // StreamBody's Foundation Data indexing. The loopback fixture also splits
  // Chinese/emoji bytes across callbacks; JS-only streams cannot accept this.
  func testNativeAccountCloudStreamSurvivesSplitUTF8AndSecondSend() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    try control("review=approved&pc=hide&api=on&start=normal&catalog=normal&model=chunked&web=normal")
    app.launch()
    let login = app.buttons["Sign in with NOSEC"].firstMatch
    XCTAssertTrue(login.waitForExistence(timeout: 15), "This case requires a fresh private app container")
    login.tap()
    XCTAssertTrue(app.staticTexts["User center"].waitForExistence(timeout: 5))
    app.buttons["Sign in with NOSEC"].tap()
    try browserLogin(app, authorize: true)
    XCTAssertTrue(app.staticTexts["native-fixture-model"].waitForExistence(timeout: 15))
    app.buttons["Chat history"].tap()
    XCTAssertTrue(app.textViews["Message"].waitForExistence(timeout: 15))

    let answer = "Native streamed 中文🙂 complete"
    for call in 1...2 {
      try send(app, "Native split stream probe \(call)")
      try waitForCount("model_chunked_completed", atLeast: call, timeout: 10)
      let replies = app.staticTexts.matching(NSPredicate(format: "label == %@", answer))
      let visible = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in
        replies.count == call && replies.allElementsBoundByIndex.allSatisfy { $0.exists }
      }, object: app)
      XCTAssertEqual(XCTWaiter.wait(for: [visible], timeout: 10), .completed, "Every native callback must preserve the complete Chinese and emoji answer")
      try waitForEnabled(app.textViews["Message"])
      XCTAssertEqual(app.state, .runningForeground, "A completed native stream must leave the app available for another send")
      XCTAssertEqual(try count("gateway_model_calls"), call)
      XCTAssertEqual(try count("model_successes"), call)
      XCTAssertEqual(try count("model_chunk_flushes"), call * 12)
      XCTAssertEqual(try count("model_chunk_failed"), 0)
      XCTAssertEqual(try count("model_chunk_cancelled"), 0)
      XCTAssertEqual(try count("connect"), 0, "The phone model call must not require a paired PC")
      record(app, "account-native-split-utf8-answer-\(call)")
    }
  }

  // Run in its own disposable app container. All model/credential settings are
  // entered through shipping UI; the private wrapper supplies only loopback CAS.
  func testNativeIndependentWebRoutesAndPermissionRecovery() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    try control("review=approved&pc=show&api=on&start=normal&catalog=normal&model=tools&web=normal")
    app.launch()
    let login = app.buttons["Sign in with NOSEC"].firstMatch
    XCTAssertTrue(login.waitForExistence(timeout: 15), "This case requires a fresh private app container")
    login.tap()
    XCTAssertTrue(app.staticTexts["User center"].waitForExistence(timeout: 5))
    app.buttons["Sign in with NOSEC"].tap()
    try browserLogin(app, authorize: true)
    XCTAssertTrue(app.staticTexts["native-fixture-model"].waitForExistence(timeout: 15))

    try choosePreference(app, label: "Phone chat Web tools", option: "Cloud server")
    try assertPreference(app, label: "PC conversation Web tools", option: "Follow this PC's settings")
    try choosePreference(app, label: "PC conversation Web tools", option: "Run on this PC")
    try assertPreference(app, label: "Phone chat Web tools", option: "Cloud server")
    try choosePreference(app, label: "PC conversation Web tools", option: "Follow this PC's settings")
    record(app, "web-independent-direct-and-pc-preferences")

    app.buttons["Chat history"].tap()
    XCTAssertFalse(app.buttons["Web settings"].exists)
    try chooseModel(app, name: "native-fixture-model", source: "Server · Native gateway")
    try send(app, "Native server Web proof")
    XCTAssertTrue(app.staticTexts["Native cloud evidence answer"].waitForExistence(timeout: 20))
    XCTAssertEqual(try count("gateway_model_calls"), 3)
    XCTAssertEqual(try count("web_search"), 1)
    XCTAssertEqual(try count("web_fetch"), 1)
    record(app, "web-gateway-model-cloud-functions")

    app.buttons["Back"].firstMatch.tap()
    app.buttons["Menu"].tap()
    app.menuItems["Models"].tap()
    if app.buttons["Add a model"].exists && !app.textFields["Name"].exists { app.buttons["Add a model"].tap() }
    try enterField(app, label: "Name", value: "Native personal")
    try enterField(app, label: "Base URL", value: fixtureOrigin + "/personal/v1")
    try enterField(app, label: "API key", value: "native-personal-key", secure: true)
    try enterField(app, label: "Default model", value: "native-personal-model")
    let save = app.buttons["Save"].firstMatch
    try reveal(app, save)
    save.tap()
    try chooseModel(app, name: "native-personal-model", source: "Native personal")
    try send(app, "Native personal Web proof")
    XCTAssertTrue(app.staticTexts["Native cloud evidence answer"].waitForExistence(timeout: 20))
    XCTAssertEqual(try count("personal_model_calls"), 3)
    XCTAssertEqual(try count("web_search"), 2)
    XCTAssertEqual(try count("web_fetch"), 2)
    XCTAssertEqual(try count("personal_mobile_token_leak"), 0)
    XCTAssertFalse(app.buttons["Web settings"].exists)
    XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Model source")).firstMatch.exists)
    record(app, "web-personal-model-independent-cloud-credential")

    try control("web=denied")
    try send(app, "Native denied Web proof")
    let denial = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Cloud Web tool access is unavailable.")).firstMatch
    XCTAssertTrue(denial.waitForExistence(timeout: 12), "A Web-only 403 must remain visible in the current conversation")
    XCTAssertEqual(try count("web_denied"), 1)
    let deniedModelCalls = try count("personal_model_calls")
    app.buttons["User center"].tap()
    try choosePreference(app, label: "Phone chat Web tools", option: "Off: ordinary model chat")
    try assertPreference(app, label: "PC conversation Web tools", option: "Follow this PC's settings")
    app.buttons["Close"].firstMatch.tap()
    XCTAssertTrue(denial.waitForExistence(timeout: 5), "Closing settings must restore the same denied conversation")
    try send(app, "Native ordinary recovery proof")
    XCTAssertTrue(app.staticTexts["Native ordinary answer"].waitForExistence(timeout: 12))
    XCTAssertEqual(try count("personal_model_calls"), deniedModelCalls + 1)
    XCTAssertEqual(try count("web_denied"), 1, "Off must not retry the denied cloud tool")
    record(app, "web-denied-visible-off-recovers-same-conversation")

    app.buttons["User center"].tap()
    try choosePreference(app, label: "Phone chat Web tools", option: "Cloud server")
    try assertPreference(app, label: "PC conversation Web tools", option: "Follow this PC's settings")
    app.buttons["Close"].firstMatch.tap()
    try control("web=stall")
    try send(app, "Native Web cancellation proof")
    try waitForCount("web_stall_started", atLeast: 1, timeout: 12)
    let stop = app.buttons["Stop"].firstMatch
    XCTAssertTrue(stop.waitForExistence(timeout: 5))
    XCTAssertTrue(stop.isHittable)
    stop.tap()
    try waitForCount("web_cancelled", atLeast: 1, timeout: 8)
    XCTAssertEqual(try count("web_completed"), 0)
    XCTAssertEqual(try count("personal_model_calls"), deniedModelCalls + 2, "Stop must prevent another model continuation")
    try waitForEnabled(app.textViews["Message"])
    record(app, "web-native-http-stop-cancels-without-continuation")

    try control("web=normal")
    app.buttons["User center"].tap()
    try assertPreference(app, label: "Phone chat Web tools", option: "Cloud server")
    try assertPreference(app, label: "PC conversation Web tools", option: "Follow this PC's settings")
    let connect = app.buttons["Connect Native PC"]
    try reveal(app, connect)
    connect.tap()
    let seed = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "Native PC conversation")).firstMatch
    XCTAssertTrue(seed.waitForExistence(timeout: 20))
    let pcRoute = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Follow PC (last refreshed)")).firstMatch
    XCTAssertFalse(app.buttons["Web settings"].exists)
    XCTAssertFalse(pcRoute.exists)
    app.buttons["Menu"].tap()
    app.buttons["User center"].tap()
    XCTAssertTrue(pcRoute.waitForExistence(timeout: 12))
    let pcExecution = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Run on this PC")).firstMatch
    XCTAssertTrue(pcExecution.exists, "Settings must show the anonymous PC's local execution snapshot")
    record(app, "web-pc-routing-in-user-center")
    app.buttons["Close"].tap()
    seed.tap()
    XCTAssertFalse(app.buttons["Web settings"].exists)
    XCTAssertFalse(pcRoute.exists)
    app.buttons["User center"].tap()
    XCTAssertTrue(pcRoute.waitForExistence(timeout: 5))
    app.buttons["Close"].tap()
    try send(app, "Native inherited PC proof")
    let persisted = try waitForPCWebTurn(timeout: 12)
    XCTAssertEqual(persisted["pc_thread_web_execution"] as? String, "", "Phone inheritance must not persist a cloud override on the PC")
    XCTAssertEqual(persisted["pc_turn_web_execution"] as? String, "auto", "The PC owns resolution of its default automatic mode")
    let pcStatus = try XCTUnwrap(persisted["pc_web_status"] as? [String: Any])
    XCTAssertEqual(pcStatus["execution"] as? String, "local")
    XCTAssertGreaterThan(try count("peer_messages"), 0)
    XCTAssertEqual(try count("personal_mobile_token_leak"), 0)
    XCTAssertEqual(try count("personal_cookie_leak"), 0)
    XCTAssertEqual(try count("native_cookie_leak"), 0)
    XCTAssertEqual(try count("invalid_model_contract"), 0)
    XCTAssertEqual(try count("invalid_web_contract"), 0)
    record(app, "web-mobile-cloud-keeps-pc-inherited-local-route")
  }

  func testNativeManagerCapContinueAndStopUseTheOriginalPCTurn() throws {
    continueAfterFailure = false
    let app = XCUIApplication()
    try control("review=approved&pc=show&api=on&start=normal&catalog=normal&model=normal&web=normal")
    app.launch()
    let login = app.buttons["Sign in with NOSEC"].firstMatch
    XCTAssertTrue(login.waitForExistence(timeout: 15), "This case requires a fresh private app container")
    login.tap()
    XCTAssertTrue(app.staticTexts["User center"].waitForExistence(timeout: 5))
    app.buttons["Sign in with NOSEC"].tap()
    try browserLogin(app, authorize: true)
    XCTAssertTrue(app.staticTexts["native-fixture-model"].waitForExistence(timeout: 15))
    let connect = app.buttons["Connect Native PC"]
    try reveal(app, connect)
    connect.tap()
    let seed = app.buttons["Open Native PC conversation"]
    XCTAssertTrue(seed.waitForExistence(timeout: 20), "The actual account PC must answer over encrypted RPC")
    let peersBefore = try count("peer_messages")
    for proceed in [true, false] {
      let started = try capData("/api/cap/start", method: "POST")
      let turn = try XCTUnwrap(started["turn_id"] as? String)
      let thread = try XCTUnwrap(started["thread_id"] as? String)
      let title = try XCTUnwrap(started["title"] as? String)
      let paused = try waitForCapState(turn, phase: "durable pause") { state in
        state["awaiting_continue"] as? Bool == true && (state["max_iterations_seq"] as? Int ?? 0) > 0
      }
      let initialCapSeq = try XCTUnwrap(paused["max_iterations_seq"] as? Int)
      XCTAssertEqual(paused["thread_id"] as? String, thread)
      XCTAssertEqual(paused["turn_count"] as? Int, 1)
      let open = app.buttons["Open " + title]
      XCTAssertTrue(open.waitForExistence(timeout: 15))
      XCTAssertTrue(open.isHittable)
      open.tap()
      let prompt = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Reached the manager tool-round limit")).firstMatch
      XCTAssertTrue(prompt.waitForExistence(timeout: 12))
      let more = app.buttons["Continue for more tool rounds"]
      let stop = app.buttons["Stop at the tool-round limit"]
      try waitForEnabled(more)
      recordCap(app, proceed ? "cap-visible" : "cap-second-visible", state: paused)
      app.buttons["Back"].tap()
      XCTAssertTrue(open.waitForExistence(timeout: 12))
      open.tap()
      try waitForEnabled(more)
      XCTAssertTrue(more.isHittable)
      XCTAssertTrue(stop.isHittable)
      recordCap(app, proceed ? "cap-reopened" : "cap-second-reopened", state: try capData("/api/cap/state"))
      var stoppedForCleanup = false
      if proceed {
        more.tap()
        let resumed = try waitForCapState(turn, phase: "same-turn manager generation") { state in
          (state["max_iterations_continued_seq"] as? Int ?? 0) > initialCapSeq &&
          state["manager_generation_after_continue"] as? Bool == true &&
          (state["running"] as? Bool != true || state["awaiting_continue"] as? Bool == true)
        }
        XCTAssertEqual(resumed["turn_count"] as? Int, 1, "Continue must retain the original PC turn")
        XCTAssertEqual(resumed["thread_id"] as? String, thread)
        XCTAssertEqual(resumed["continued_count"] as? Int, 1, "One native decision grants one continuation")
        recordCap(app, "cap-original-turn-continued", state: resumed)
        if resumed["running"] as? Bool == true {
          try waitForEnabled(stop)
          XCTAssertTrue(stop.isHittable)
          stop.tap()
          stoppedForCleanup = true
        }
      } else { stop.tap() }
      // Engine publishes its terminal event before FinishTurn. Keep waiting
      // while the observer still sees the old durable turn status.
      let ended = try waitForCapState(turn, phase: "terminal original turn") { state in
        state["running"] as? Bool != true && (state["terminal_seq"] as? Int ?? 0) > 0 &&
        ["done", "error", "cancelled"].contains(state["turn_status"] as? String ?? "")
      }
      XCTAssertEqual(ended["turn_count"] as? Int, 1, "A cap decision must not send another user turn")
      XCTAssertEqual(ended["thread_id"] as? String, thread)
      XCTAssertEqual(ended["turn_status"] as? String, !proceed || stoppedForCleanup ? "cancelled" : "done")
      if !proceed {
        XCTAssertEqual(ended["turn_status"] as? String, "cancelled")
        XCTAssertEqual(ended["continued_count"] as? Int, 0)
        XCTAssertTrue(app.staticTexts["The turn stopped at the tool-round limit."].waitForExistence(timeout: 8))
      }
      let settled = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: more)
      XCTAssertEqual(XCTWaiter.wait(for: [settled], timeout: 8), .completed)
      XCTAssertFalse(stop.exists)
      recordCap(app, proceed ? "cap-continued-case-cleaned" : "cap-original-turn-cancelled", state: ended)
      app.buttons["Back"].tap()
    }
    XCTAssertGreaterThan(try count("peer_messages"), peersBefore)
  }

  private func capData(_ path: String, method: String = "GET") throws -> [String: Any] {
    let response = try read(path, method: method)
    return try XCTUnwrap(response["data"] as? [String: Any])
  }

  private func waitForCapState(_ turn: String, phase: String, ready: ([String: Any]) -> Bool) throws -> [String: Any] {
    let deadline = Date().addingTimeInterval(15)
    repeat {
      let state = try capData("/api/cap/state")
      XCTAssertEqual(state["turn_id"] as? String, turn, "The observer must stay bound to the original turn")
      if ready(state) { return state }
      Thread.sleep(forTimeInterval: 0.1)
    } while Date() < deadline
    XCTFail("The actual PC cap did not reach " + phase)
    throw NSError(domain: "NativeAccountFixture", code: 5)
  }

  private func recordCap(_ app: XCUIApplication, _ name: String, state: [String: Any]) {
    record(app, name)
    if let data = try? JSONSerialization.data(withJSONObject: state, options: [.sortedKeys]), let json = String(data: data, encoding: .utf8) {
      let receipt = XCTAttachment(string: json)
      receipt.name = name + "-pc-cap-state"
      receipt.lifetime = .keepAlways
      add(receipt)
    }
  }

  private func waitForPCWebTurn(timeout: TimeInterval) throws -> [String: Any] {
    let deadline = Date().addingTimeInterval(timeout)
    repeat {
      let state = try read("/fixture/state")
      let data = try XCTUnwrap(state["data"] as? [String: Any])
      if data["pc_turn_web_execution"] is String { return data }
      Thread.sleep(forTimeInterval: 0.2)
    } while Date() < deadline
    XCTFail("The encrypted PC send did not persist a turn execution snapshot")
    throw NSError(domain: "NativeAccountFixture", code: 4)
  }

  private func chooseModel(_ app: XCUIApplication, name: String, source: String) throws {
    let model = app.buttons["Model"].firstMatch
    XCTAssertTrue(model.waitForExistence(timeout: 8))
    model.tap()
    let heading = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", source)).firstMatch
    XCTAssertTrue(heading.waitForExistence(timeout: 8), "The picker must identify the model source: \(source)")
    let choice = app.otherElements[name].firstMatch
    XCTAssertTrue(choice.waitForExistence(timeout: 8))
    choice.tap()
  }

  private func preference(_ app: XCUIApplication, label: String) throws -> XCUIElement {
    let choices = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", label))
    XCTAssertTrue(choices.firstMatch.waitForExistence(timeout: 8), "Missing preference: \(label)")
    let controls = choices.allElementsBoundByIndex.filter {
      $0.elementType != .staticText && !(($0.value as? String) ?? "").isEmpty
    }
    guard controls.count == 1 else {
      XCTFail("Expected one value-bearing preference control: \(label)")
      throw NSError(domain: "NativeAccountFixture", code: 5)
    }
    return controls[0]
  }

  private func assertPreference(_ app: XCUIApplication, label: String, option: String) throws {
    let control = try preference(app, label: label)
    let observed = (control.value as? String) ?? control.label
    XCTAssertTrue(observed.contains(option), "\(label) expected \(option), received \(observed)")
  }

  private func choosePreference(_ app: XCUIApplication, label: String, option: String) throws {
    let control = try preference(app, label: label)
    try reveal(app, control)
    control.tap()
    let wheel = app.pickerWheels.firstMatch
    if wheel.waitForExistence(timeout: 1) {
      wheel.adjust(toPickerWheelValue: option)
      try dismissKeyboard(app)
    } else {
      let selected = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", option)).firstMatch
      XCTAssertTrue(selected.waitForExistence(timeout: 5))
      selected.tap()
    }
    try assertPreference(app, label: label, option: option)
  }

  private func enterField(_ app: XCUIApplication, label: String, value: String, secure: Bool = false) throws {
    let field = secure ? app.secureTextFields[label].firstMatch : app.textFields[label].firstMatch
    XCTAssertTrue(field.waitForExistence(timeout: 8))
    try reveal(app, field)
    field.tap()
    field.typeText(value)
    try dismissKeyboard(app)
  }


  private func dismissKeyboard(_ app: XCUIApplication) throws {
    let introductions = app.otherElements.matching(identifier: "UIContinuousPathIntroductionView")
    if introductions.firstMatch.exists {
      guard introductions.count == 1 else {
        XCTFail("The keyboard introduction must be unique")
        throw NSError(domain: "NativeAccountFixture", code: 8)
      }
      let introduction = introductions.element(boundBy: 0)
      let buttons = introduction.buttons.matching(NSPredicate(format: "label == %@", "Continue"))
      guard buttons.count == 1 else {
        XCTFail("The observed keyboard introduction must have one Continue button")
        throw NSError(domain: "NativeAccountFixture", code: 8)
      }
      let proceed = buttons.element(boundBy: 0)
      let ready = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in proceed.exists && proceed.isHittable }, object: app)
      guard XCTWaiter.wait(for: [ready], timeout: 5) == .completed else {
        XCTFail("The keyboard introduction's Continue button is not hittable")
        throw NSError(domain: "NativeAccountFixture", code: 8)
      }
      proceed.tap()
      XCTAssertTrue(introduction.waitForNonExistence(timeout: 5))
    }
    let keyboard = app.keyboards.firstMatch
    let wheel = app.pickerWheels.firstMatch
    let buttons = app.toolbars.buttons.matching(NSPredicate(format: "label == %@ OR label == %@", "Done", "selected"))
    let deadline = ProcessInfo.processInfo.systemUptime + 5
    repeat {
      let controls = buttons.allElementsBoundByIndex.filter { $0.exists && $0.isHittable }
      if controls.count == 1 {
        controls[0].tap()
        let hidden = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in !keyboard.exists && !wheel.exists }, object: app)
        guard XCTWaiter.wait(for: [hidden], timeout: 5) == .completed else {
          XCTFail("The observed keyboard dismiss action did not close the input view")
          throw NSError(domain: "NativeAccountFixture", code: 9)
        }
        return
      }
      if controls.count > 1 {
        XCTFail("The keyboard has ambiguous dismiss controls")
        throw NSError(domain: "NativeAccountFixture", code: 9)
      }
      if !keyboard.exists && !wheel.exists { return }
      RunLoop.current.run(until: Date().addingTimeInterval(0.1))
    } while ProcessInfo.processInfo.systemUptime < deadline
    XCTFail("The keyboard must have one visible Done or selected dismiss button")
    throw NSError(domain: "NativeAccountFixture", code: 9)
  }

  private func reveal(_ app: XCUIApplication, _ element: XCUIElement) throws {
    for _ in 0..<4 {
      if element.isHittable { return }
      app.swipeUp()
    }
    XCTAssertTrue(element.isHittable)
  }

  private func browserLogin(_ app: XCUIApplication, authorize: Bool) throws {
    let signIn = app.buttons["Sign in to test NOSEC"]
    XCTAssertTrue(signIn.waitForExistence(timeout: 15), "The actual Capacitor Browser must display the loopback CAS fixture")
    signIn.tap()
    if authorize {
      let allow = app.buttons["Authorize test device"]
      XCTAssertTrue(allow.waitForExistence(timeout: 15))
      allow.tap()
    } else {
      XCTAssertTrue(app.staticTexts["Waiting for administrator approval"].waitForExistence(timeout: 12))
    }
    record(app, authorize ? "account-native-browser-authorized" : "account-native-browser-pending")
    let close = app.otherElements["TopBrowserBar"].buttons.matching(identifier: "Close")
    let done = app.buttons.matching(identifier: "Done")
    let deadline = ProcessInfo.processInfo.systemUptime + 5
    repeat {
      let controls = (close.allElementsBoundByIndex + done.allElementsBoundByIndex).filter { $0.exists && $0.isHittable }
      if controls.count == 1 {
        controls[0].tap()
        return
      }
      if controls.count > 1 {
        XCTFail("The native browser has ambiguous return controls")
        throw NSError(domain: "NativeAccountFixture", code: 6)
      }
      RunLoop.current.run(until: Date().addingTimeInterval(0.1))
    } while ProcessInfo.processInfo.systemUptime < deadline
    XCTFail("The native browser must offer one visible Close or Done button")
    throw NSError(domain: "NativeAccountFixture", code: 7)
  }

  private func assertReviewOnly(_ app: XCUIApplication) throws {
    XCTAssertFalse(app.buttons["Menu"].exists)
    XCTAssertFalse(app.buttons["Connect a model"].exists)
    XCTAssertFalse(app.textViews["Message"].exists)
    XCTAssertFalse(app.staticTexts["Your computers"].exists)
    XCTAssertFalse(app.buttons["Close"].exists)
  }

  private func send(_ app: XCUIApplication, _ text: String) throws {
    let message = app.textViews["Message"]
    try waitForEnabled(message)
    message.tap()
    message.typeText(text)
    let send = app.buttons["Send"]
    try waitForEnabled(send)
    XCTAssertTrue(send.isHittable)
    send.tap()
  }

  private func waitForEnabled(_ element: XCUIElement, timeout: TimeInterval = 8) throws {
    let ready = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND enabled == true"), object: element)
    XCTAssertEqual(XCTWaiter.wait(for: [ready], timeout: timeout), .completed)
  }

  private func control(_ query: String) throws {
    _ = try read("/fixture/control?" + query)
  }

  private func counts() throws -> [String: Int] {
    let result = try read("/fixture/state")
    let data = try XCTUnwrap(result["data"] as? [String: Any])
    return try XCTUnwrap(data["counts"] as? [String: Int])
  }

  // The fixture intentionally returns sparse counters: an absent event is zero.
  private func count(_ key: String) throws -> Int {
    try counts()[key, default: 0]
  }

  private func waitForCount(_ key: String, atLeast minimum: Int, timeout: TimeInterval) throws {
    let deadline = Date().addingTimeInterval(timeout)
    repeat {
      if try count(key) >= minimum { return }
      Thread.sleep(forTimeInterval: 0.2)
    } while Date() < deadline
    XCTFail("Fixture counter \(key) did not reach \(minimum)")
    throw NSError(domain: "NativeAccountFixture", code: 3)
  }

  private func read(_ path: String, method: String = "GET") throws -> [String: Any] {
    let url = try XCTUnwrap(URL(string: fixtureOrigin + path))
    var request = URLRequest(url: url)
    request.httpMethod = method
    let done = expectation(description: "fixture response")
    var value: [String: Any]?
    var failure: Error?
    let task = URLSession.shared.dataTask(with: request) { data, response, error in
      defer { done.fulfill() }
      if let error = error { failure = error; return }
      guard let response = response as? HTTPURLResponse, response.statusCode == 200, let data = data else {
        failure = NSError(domain: "NativeAccountFixture", code: 1)
        return
      }
      do { value = try JSONSerialization.jsonObject(with: data) as? [String: Any] }
      catch { failure = error }
    }
    task.resume()
    let result = XCTWaiter.wait(for: [done], timeout: 8)
    if result != .completed { task.cancel(); throw NSError(domain: "NativeAccountFixture", code: 2) }
    if let failure = failure { throw failure }
    return try XCTUnwrap(value)
  }

  override func tearDownWithError() throws {
    if (testRun?.failureCount ?? 0) > 0 { record(XCUIApplication(), "account-native-failure") }
    XCUIApplication().terminate()
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
    if let observed = try? counts(), let data = try? JSONSerialization.data(withJSONObject: observed, options: [.sortedKeys]), let json = String(data: data, encoding: .utf8) {
      let receipt = XCTAttachment(string: json)
      receipt.name = name + "-fixture-counts"
      receipt.lifetime = .keepAlways
      add(receipt)
    }
  }
}
