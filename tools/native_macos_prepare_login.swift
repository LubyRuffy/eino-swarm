// Native OS leaf for a case-owned preparation actor. Credentials use stdin only.
import Foundation
import ApplicationServices
import CoreFoundation
import Darwin

func strictBool(_ value: Any?) -> Bool? {
    guard let n = value as? NSNumber, CFGetTypeID(n) == CFBooleanGetTypeID() else { return nil }
    return n.boolValue
}
func session() -> [String: Any] {
    let d = CGSessionCopyCurrentDictionary() as? [String: Any] ?? [:]
    let present = d.keys.contains("CGSSessionScreenIsLocked")
    return ["user": d["kCGSSessionUserNameKey"] ?? NSNull(),
            "uid": d["kCGSSessionUserIDKey"] ?? NSNull(),
            "on_console": strictBool(d["kCGSSessionOnConsoleKey"]) as Any? ?? NSNull(),
            "login_done": strictBool(d["kCGSessionLoginDoneKey"]) as Any? ?? NSNull(),
            "audit_id": d["kCGSSessionAuditIDKey"] ?? NSNull(),
            "session_uuid": d["CGSSessionUniqueSessionUUID"] ?? NSNull(),
            "lock_key_present": present,
            "locked": strictBool(d["CGSSessionScreenIsLocked"]) as Any? ?? NSNull()]
}
func emit(_ value: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: value, options: [.sortedKeys]) else { exit(2) }
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data([10]))
}
func virtualGuest() -> Bool {
    var present: Int32 = 0
    var size = MemoryLayout<Int32>.size
    return sysctlbyname("kern.hv_vmm_present", &present, &size, nil, 0) == 0 && present == 1
}
guard virtualGuest(), CommandLine.arguments.count >= 2 else { exit(2) }
if CommandLine.arguments[1] == "session" {
    guard CommandLine.arguments.count == 2 else { exit(2) }
    emit(session()); exit(0)
}
guard CommandLine.arguments.count == 3, CommandLine.arguments[1] == "input",
      let raw = CommandLine.arguments[2].data(using: .utf8),
      let target = (try? JSONSerialization.jsonObject(with: raw)) as? [String: Any],
      let expected = target["session"] as? [String: Any],
      let capturedRaw = target["captured_raw"] as? Double,
      let capturedWall = target["captured_wall"] as? Double,
      let width = target["width"] as? Int, let height = target["height"] as? Int,
      let x = target["password_x"] as? Double, let y = target["password_y"] as? Double,
      AXIsProcessTrusted(), width > 0, height > 0, x >= 0, y >= 0,
      x < Double(width), y < Double(height) else { exit(2) }
func guarded() -> Bool {
    let s = session()
    var clock = timespec()
    guard clock_gettime(CLOCK_MONOTONIC_RAW, &clock) == 0 else { return false }
    let rawAge = Double(clock.tv_sec) + Double(clock.tv_nsec) / 1_000_000_000 - capturedRaw
    let wallAge = Date().timeIntervalSince1970 - capturedWall
    return CGDisplayPixelsWide(CGMainDisplayID()) == width && CGDisplayPixelsHigh(CGMainDisplayID()) == height
        && rawAge >= 0 && rawAge < 60 && wallAge >= 0 && wallAge < 60 && abs(rawAge-wallAge) <= 1
        && s["lock_key_present"] as? Bool == true && s["locked"] as? Bool == true
        && (s as NSDictionary).isEqual(to: expected.merging(["lock_key_present": true, "locked": true]) { _, new in new })
        && expected["uid"] as? Int == Int(getuid())
}
guard guarded() else { exit(2) }
let data = FileHandle.standardInput.readDataToEndOfFile()
guard let password = String(data: data, encoding: .utf8), !password.isEmpty, password.utf16.count < 128 else { exit(3) }
let keys: [Character: CGKeyCode] = ["a":0,"s":1,"d":2,"f":3,"h":4,"g":5,"z":6,"x":7,"c":8,"v":9,"b":11,"q":12,"w":13,"e":14,"r":15,"y":16,"t":17,"1":18,"2":19,"3":20,"4":21,"6":22,"5":23,"9":25,"7":26,"8":28,"0":29,"o":31,"u":32,"i":34,"p":35,"l":37,"j":38,"k":40,"n":45,"m":46]
guard password.allSatisfy({ keys[$0] != nil }) else { exit(3) }
func post(_ event: CGEvent?) {
    guard guarded(), let value = event else { exit(4) }
    value.post(tap: .cghidEventTap)
}
let point = CGPoint(x: x, y: y)
post(CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown, mouseCursorPosition: point, mouseButton: .left))
post(CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp, mouseCursorPosition: point, mouseButton: .left))
usleep(150000)
for character in password {
    post(CGEvent(keyboardEventSource: nil, virtualKey: keys[character]!, keyDown: true))
    post(CGEvent(keyboardEventSource: nil, virtualKey: keys[character]!, keyDown: false))
    usleep(80000)
}
post(CGEvent(keyboardEventSource: nil, virtualKey: 36, keyDown: true))
post(CGEvent(keyboardEventSource: nil, virtualKey: 36, keyDown: false))
let end = ProcessInfo.processInfo.systemUptime + 8
while session()["locked"] as? Bool == true && ProcessInfo.processInfo.systemUptime < end { usleep(100000) }
let unlocked = session()["locked"] as? Bool == false
emit(["input_attempted": true, "credential_recorded": false, "unlocked": unlocked])
exit(unlocked ? 0 : 1)
