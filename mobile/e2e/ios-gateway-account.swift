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
    let center = app.menuItems["User center"]
    XCTAssertTrue(center.waitForExistence(timeout: 5))
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
    let done = app.buttons["Done"].firstMatch
    XCTAssertTrue(done.waitForExistence(timeout: 5), "The native browser must offer a return to the app")
    done.tap()
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

  private func read(_ path: String) throws -> [String: Any] {
    let url = try XCTUnwrap(URL(string: fixtureOrigin + path))
    let done = expectation(description: "fixture response")
    var value: [String: Any]?
    var failure: Error?
    let task = URLSession.shared.dataTask(with: url) { data, response, error in
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
