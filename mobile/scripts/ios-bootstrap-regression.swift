import Foundation
import WebKit

@main @MainActor struct BootstrapRegression {
    static func main() throws {
        let arguments = CommandLine.arguments
        let scenario = arguments[1]
        let input = try String(contentsOfFile: arguments[2], encoding: .utf8)
        let output = URL(fileURLWithPath: arguments[3])
        let cookies = arguments[4] == "true"
        let http = arguments[5] == "true"
        let controller = WKUserContentController()
        let before = WKUserScript(source: "window.beforeBridge = true", injectionTime: .atDocumentStart,
                                  forMainFrameOnly: false, in: .world(name: "independent-probe"))
        let after = WKUserScript(source: "window.afterBridge = true", injectionTime: .atDocumentEnd,
                                 forMainFrameOnly: true)
        let source = scenario == "drift" ? input.replacingOccurrences(of: "const isCookiesEnabled = prompt", with: "const changedConfig = prompt") : input
        let bridge = WKUserScript(source: source,
                                  injectionTime: scenario == "bad-time" ? .atDocumentEnd : .atDocumentStart,
                                  forMainFrameOnly: scenario != "all-frames")
        controller.addUserScript(before)
        if scenario != "missing" { controller.addUserScript(bridge) }
        if scenario == "duplicate" { controller.addUserScript(bridge) }
        controller.addUserScript(after)
        let original = controller.userScripts.map { $0 }

        if ["missing", "duplicate", "bad-time", "all-frames", "drift"].contains(scenario) {
            do {
                try IOSBootstrap.install(on: controller, cookiesEnabled: cookies, httpEnabled: http)
                fatalError("Unsupported SDK registration must refuse before mutation")
            } catch is IOSBootstrap.Failure {
                let unchanged = controller.userScripts
                precondition(unchanged.count == original.count)
                precondition(zip(unchanged, original).allSatisfy { $0 === $1 })
            }
        } else {
            try IOSBootstrap.install(on: controller, cookiesEnabled: cookies, httpEnabled: http)
            let registered = controller.userScripts.map { $0 }
            precondition(registered.count == 3 && registered[0] === original[0] && registered[2] === original[2])
            precondition(registered[1] !== original[1])
            precondition(registered[1].injectionTime == original[1].injectionTime && registered[1].isForMainFrameOnly)
            try registered[1].source.write(to: output, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: output.path)
            try IOSBootstrap.install(on: controller, cookiesEnabled: cookies, httpEnabled: http)
            precondition(zip(controller.userScripts, registered).allSatisfy { $0 === $1 })
        }
        print("Swift registration and refusal checks passed: \(scenario)")
    }
}
