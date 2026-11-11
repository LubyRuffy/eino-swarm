import Foundation
import CryptoKit
import WebKit

/// Backport capacitor#8689 at the script-registration boundary. SwiftPM uses
/// a binary SDK, so editing npm's Swift sources cannot fix the installed app.
@MainActor enum IOSBootstrap {
    enum Failure: Error {
        case missingBridge, multipleBridges, unsupportedInjection, unsupportedSource
    }

    private static let marker = "// zwai-ios-static-config-bootstrap\n"
    // These identify the pinned 7.6.9 SwiftPM framework resource and the same
    // body with only the two static config checks changed. Review on SDK upgrades.
    private static let legacyDigest = "48b589d954751db7ebad3f5ef6983b523f96bab4c2db0b0f85b8a311b0123a5e"
    private static let adaptedDigest = "d262ee981cb06f7242f6b0b38b700fc70105b02ced7431c1e9d0c910095d3503"

    static func install(on controller: WKUserContentController, cookiesEnabled: Bool, httpEnabled: Bool) throws {
        let scripts = controller.userScripts.map { $0 }
        let indices = scripts.indices.filter {
            let source = scripts[$0].source
            return source.contains("var nativeBridge = (function (exports)")
                && source.contains("const initLogger = (win, cap) => {")
        }
        guard !indices.isEmpty else { throw Failure.missingBridge }
        guard indices.count == 1 else { throw Failure.multipleBridges }
        let index = indices[0]
        let original = scripts[index]
        guard original.injectionTime == .atDocumentStart, original.isForMainFrameOnly else {
            throw Failure.unsupportedInjection
        }
        let source = try adaptedSource(original.source, cookiesEnabled: cookiesEnabled, httpEnabled: httpEnabled)
        guard source != original.source else { return }
        // Capacitor's JSExport.injectFile creates this SDK script in the page
        // world using the default initializer. Keep every other script object,
        // including isolated-world scripts, and their original order intact.
        var updated = scripts
        updated[index] = WKUserScript(source: source, injectionTime: original.injectionTime,
                                      forMainFrameOnly: original.isForMainFrameOnly)
        controller.removeAllUserScripts()
        for script in updated { controller.addUserScript(script) }
    }

    private static func adaptedSource(_ source: String, cookiesEnabled: Bool, httpEnabled: Bool) throws -> String {
        let prefix = marker + "window.CapacitorNativeConfig = { cookiesEnabled: \(cookiesEnabled), httpEnabled: \(httpEnabled) };\n"
        if source.hasPrefix(prefix) {
            guard digest(String(source.dropFirst(prefix.count))) == adaptedDigest else { throw Failure.unsupportedSource }
            return source
        }
        guard digest(source) == legacyDigest else { throw Failure.unsupportedSource }
        var result = source
        let checks = [("cookies", "CapacitorCookies.isEnabled", "isCookiesEnabled", "cookiesEnabled"),
                      ("http", "CapacitorHttp", "isHttpEnabled", "httpEnabled")]
        for (label, type, local, key) in checks {
            let lines = [
                "// Use prompt to synchronously get capacitor \(label) config.",
                "// https://stackoverflow.com/questions/29249132/wkwebview-complex-communication-between-javascript-native-code/49474323#49474323",
                "const payload = {", "    type: '\(type)',", "};",
                "const \(local) = prompt(JSON.stringify(payload));",
                "if (\(local) === 'true') {"
            ]
            let before = lines.map { "                    " + $0 }.joined(separator: "\n")
            let after = "                    if (win.CapacitorNativeConfig && win.CapacitorNativeConfig.\(key) === true) {"
            guard result.components(separatedBy: before).count == 2,
                  result.components(separatedBy: after).count == 1 else { throw Failure.unsupportedSource }
            result = result.replacingOccurrences(of: before, with: after)
        }
        guard digest(result) == adaptedDigest else { throw Failure.unsupportedSource }
        return prefix + result
    }

    private static func digest(_ source: String) -> String {
        SHA256.hash(data: Data(source.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}
