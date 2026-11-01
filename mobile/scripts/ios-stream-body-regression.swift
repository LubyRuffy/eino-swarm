import Capacitor
import Darwin
import Foundation

private func check(_ condition: @autoclosure () -> Bool, _ message: String) {
    guard condition() else { fatalError(message) }
}

private func decode(_ chunks: [Data]) -> String {
    var pending = Data()
    var result = ""
    for chunk in chunks {
        pending.append(chunk)
        result += takeUTF8(&pending, end: false)
    }
    result += takeUTF8(&pending, end: true)
    check(pending.isEmpty, "the final callback must drain pending bytes")
    return result
}

private func sequential() {
    let pieces = ["data: first response\n\n", "x", "", "y", "a longer packet\n", "z"]
    check(decode(pieces.map { Data($0.utf8) }) == pieces.joined(), "short chunks after a drained buffer")
    let bytes = Data("a response that arrives one byte at a time".utf8)
    check(decode(bytes.map { Data([$0]) }) == String(data: bytes, encoding: .utf8), "one-byte chunks")
}

private func nonzero() {
    let prefix = Data(repeating: 0x61, count: 64)
    let text = "中文🙂tail"
    let bytes = Data(text.utf8)
    for cut in 0...bytes.count {
        var pending = (prefix + bytes.prefix(cut)).dropFirst(prefix.count)
        check(pending.startIndex == prefix.count, "fixture must keep a nonzero Data index")
        let first = takeUTF8(&pending, end: false)
        pending.append(bytes.dropFirst(cut))
        let second = takeUTF8(&pending, end: false)
        check(first + second + takeUTF8(&pending, end: true) == text, "slice byte offset \(cut)")
    }
}

private func unicode() {
    // Every two- and three-packet boundary includes splits inside a code point.
    // Prefixing a complete token also exercises the nonzero buffer indices left
    // by the previous callback before the multi-byte token arrives.
    for text in ["é", "中文", "🙂🚀", "a中文🙂終z"] {
        let bytes = Data(text.utf8)
        for first in 0...bytes.count {
            for second in first...bytes.count {
                let chunks = [Data("token:".utf8), Data(bytes.prefix(first)),
                              Data(bytes.dropFirst(first).prefix(second - first)), Data(bytes.dropFirst(second))]
                check(decode(chunks) == "token:" + text, "UTF-8 boundaries \(first), \(second) in \(text)")
            }
        }
        check(decode(bytes.map { Data([$0]) }) == text, "single-byte UTF-8 chunks")
    }
}

private func tails() {
    var empty = Data()
    check(takeUTF8(&empty, end: false).isEmpty, "empty data callback")
    check(takeUTF8(&empty, end: true).isEmpty, "empty completion callback")
    for bytes: [UInt8] in [[0xe4], [0xe4, 0xb8], [0xf0, 0x9f, 0x99], [0x80], [0x80, 0x80]] {
        var pending = Data(bytes)
        check(takeUTF8(&pending, end: false).isEmpty, "partial UTF-8 must wait")
        check(pending == Data(bytes), "partial bytes must stay for the next callback")
        check(takeUTF8(&pending, end: true).isEmpty, "incomplete final bytes retain the existing drop behavior")
        check(pending.isEmpty, "completion must drain the incomplete tail")
    }
    for bytes: [UInt8] in [[0xff], [0x80, 0x80, 0x80, 0x80], [0xc0, 0x80]] {
        var pending = Data(bytes)
        check(takeUTF8(&pending, end: false).isEmpty, "invalid UTF-8 must not become model text")
        check(pending.isEmpty, "existing invalid-byte handling must still drain a complete malformed sequence")
    }
}

private final class RecordingStreamBody: StreamBodyPlugin {
    private let received = NSCondition()
    private var receivedBytes = 0
    private var callbackCount = 0

    var callbacks: Int {
        received.lock()
        defer { received.unlock() }
        return callbackCount
    }

    func waitForBytes(_ count: Int) -> Bool {
        received.lock()
        defer { received.unlock() }
        let deadline = Date().addingTimeInterval(3)
        while receivedBytes < count {
            if !received.wait(until: deadline) { return receivedBytes >= count }
        }
        return true
    }

    override func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        super.urlSession(session, dataTask: dataTask, didReceive: data)
        received.lock()
        receivedBytes += data.count
        callbackCount += 1
        received.broadcast()
        received.unlock()
    }
}

private func socketWrite(_ fd: Int32, _ bytes: Data) {
    bytes.withUnsafeBytes { buffer in
        var offset = 0
        while offset < buffer.count {
            let count = Darwin.write(fd, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
            check(count > 0, "fixture socket write failed")
            offset += count
        }
    }
}

private func urlsession() {
    let listener = socket(AF_INET, SOCK_STREAM, 0)
    check(listener >= 0, "fixture socket")
    defer { close(listener) }
    var address = sockaddr_in()
    address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
    address.sin_family = sa_family_t(AF_INET)
    address.sin_addr = in_addr(s_addr: inet_addr("127.0.0.1"))
    let bound = withUnsafePointer(to: &address) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(listener, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
    }
    check(bound == 0 && listen(listener, 1) == 0, "fixture listen")
    var size = socklen_t(MemoryLayout<sockaddr_in>.size)
    let named = withUnsafeMutablePointer(to: &address) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(listener, $0, &size) }
    }
    check(named == 0, "fixture port")

    let plugin = RecordingStreamBody()
    let chunks = [Data("data: first response\n\n".utf8), Data("x".utf8),
                  Data([0xe4, 0xb8]), Data([0xad]), Data("🙂".utf8), Data("z".utf8)]
    let serverDone = DispatchSemaphore(value: 0)
    DispatchQueue.global().async {
        let connection = accept(listener, nil, nil)
        check(connection >= 0, "fixture accept")
        defer { close(connection); serverDone.signal() }
        var request = [UInt8](repeating: 0, count: 4096)
        check(read(connection, &request, request.count) > 0, "fixture request")
        socketWrite(connection, Data("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Type: text/event-stream\r\nConnection: close\r\n\r\n".utf8))
        var sentBytes = 0
        for chunk in chunks {
            socketWrite(connection, Data(String(chunk.count, radix: 16).utf8) + Data("\r\n".utf8) + chunk + Data("\r\n".utf8))
            // A later packet cannot hide the short or incomplete packet through
            // TCP coalescing: the real delegate must consume each one first.
            sentBytes += chunk.count
            check(plugin.waitForBytes(sentBytes), "real URLSession delegate did not consume its packet")
        }
        socketWrite(connection, Data("0\r\n\r\n".utf8))
    }

    var events: [(String, [String: Any])] = []
    var resolved: [String: Any]?
    var failure: String?
    plugin.onEvent = { events.append(($0, $1)) }
    let call = CAPPluginCall(["id": "native-regression", "url": "http://127.0.0.1:\(UInt16(bigEndian: address.sin_port))/completion",
                              "body": "{}", "timeoutMs": 3000.0])
    call.onResolve = { resolved = $0 }
    call.onReject = { failure = $0 }
    plugin.open(call)
    let deadline = Date().addingTimeInterval(10)
    while resolved == nil && failure == nil && Date() < deadline {
        RunLoop.current.run(until: Date().addingTimeInterval(0.01))
    }
    check(failure == nil, "native completion rejected: \(failure ?? "")")
    check(resolved?["status"] as? Int == 200, "native completion status")
    check(resolved?["aborted"] as? Bool == false, "native completion was aborted")
    check(events.first?.0 == "status", "HTTP status must precede native chunks")
    check(events.allSatisfy { $0.1["id"] as? String == "native-regression" }, "native event identity")
    let text = events.filter { $0.0 == "chunk" }.compactMap { $0.1["text"] as? String }.joined()
    check(text == "data: first response\n\nx中🙂z", "real delegate lost or changed response bytes")
    check(serverDone.wait(timeout: .now() + 1) == .success, "fixture server did not exit")
    check(plugin.callbacks >= chunks.count, "fixture must cross every real delegate packet boundary")
}

@main
private struct StreamBodyRegression {
    static func main() {
        switch CommandLine.arguments.last {
        case "sequential": sequential()
        case "nonzero": nonzero()
        case "unicode": unicode()
        case "tails": tails()
        case "urlsession": urlsession()
        default: fatalError("unknown scenario")
        }
        print("native stream regression passed: \(CommandLine.arguments.last ?? "")")
    }
}
