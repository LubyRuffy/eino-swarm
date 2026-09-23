import Capacitor
import Foundation

/// POSTs one http(s) body and forwards response bytes as they arrive.
/// URLSession's completion-handler data task, which CapacitorHttp uses,
/// keeps the transcript frozen until the model stops.
@objc(StreamBodyPlugin)
public class StreamBodyPlugin: CAPPlugin, CAPBridgedPlugin, URLSessionDataDelegate {
    public let identifier = "StreamBodyPlugin"
    public let jsName = "StreamBody"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "open", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "cancel", returnType: CAPPluginReturnPromise),
    ]

    private final class Job {
        let id: String
        let call: CAPPluginCall
        let session: URLSession
        var task: URLSessionDataTask?
        var status = 0
        var cancelled = false
        var settled = false
        var pending = Data()
        init(id: String, call: CAPPluginCall, session: URLSession) {
            self.id = id
            self.call = call
            self.session = session
        }
    }

    private var jobs: [String: Job] = [:]
    private let jobsLock = NSLock()

    @objc func open(_ call: CAPPluginCall) {
        guard let id = call.getString("id"), !id.isEmpty else {
            call.reject("bad-url")
            return
        }
        guard let raw = call.getString("url"),
              let url = URL(string: raw),
              let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              let host = url.host,
              !host.isEmpty
        else {
            call.reject("bad-url")
            return
        }
        jobsLock.lock()
        let exists = jobs[id] != nil
        jobsLock.unlock()
        if exists {
            call.reject("bad-url")
            return
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.httpBody = Data((call.getString("body") ?? "").utf8)
        if let headers = call.getObject("headers") {
            for (key, value) in headers {
                guard let text = value as? String else { continue }
                if key.isEmpty || key.contains("\n") || key.contains("\r") { continue }
                if text.contains("\n") || text.contains("\r") { continue }
                request.setValue(text, forHTTPHeaderField: key)
            }
        }
        if request.value(forHTTPHeaderField: "Accept-Encoding") == nil {
            request.setValue("identity", forHTTPHeaderField: "Accept-Encoding")
        }
        let timeoutMs = call.getDouble("timeoutMs") ?? 300_000
        let seconds = max(0.001, timeoutMs / 1000)
        request.timeoutInterval = seconds

        let config = URLSessionConfiguration.ephemeral
        // Idle between packets. The resource cap is the whole transfer, so a
        // long reply that is still sending is not cut off at the idle clock.
        config.timeoutIntervalForRequest = seconds
        config.timeoutIntervalForResource = 24 * 60 * 60
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.waitsForConnectivity = false
        let queue = OperationQueue()
        queue.maxConcurrentOperationCount = 1
        let session = URLSession(configuration: config, delegate: self, delegateQueue: queue)
        let job = Job(id: id, call: call, session: session)
        let task = session.dataTask(with: request)
        task.taskDescription = id
        job.task = task
        jobsLock.lock()
        jobs[id] = job
        jobsLock.unlock()
        task.resume()
    }

    @objc func cancel(_ call: CAPPluginCall) {
        let id = call.getString("id") ?? ""
        jobsLock.lock()
        let job = jobs[id]
        job?.cancelled = true
        let task = job?.task
        jobsLock.unlock()
        task?.cancel()
        call.resolve()
    }

    public func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest,
        completionHandler: @escaping (URLRequest?) -> Void
    ) {
        // A redirect would resend the bearer token. The configured URL is
        // the endpoint; refusing leaves the 3xx as the response.
        completionHandler(nil)
    }

    public func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        guard let job = job(for: dataTask) else {
            completionHandler(.cancel)
            return
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        jobsLock.lock()
        job.status = status
        jobsLock.unlock()
        emit(["id": job.id, "status": status], event: "status")
        completionHandler(.allow)
    }

    public func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        guard let job = job(for: dataTask), !data.isEmpty else { return }
        jobsLock.lock()
        job.pending.append(data)
        let text = takeUTF8(&job.pending, end: false)
        jobsLock.unlock()
        if !text.isEmpty {
            emit(["id": job.id, "text": text], event: "chunk")
        }
    }

    public func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        guard let job = job(for: task) else { return }
        jobsLock.lock()
        let tail = takeUTF8(&job.pending, end: true)
        let cancelled = job.cancelled
        jobsLock.unlock()
        if !tail.isEmpty {
            emit(["id": job.id, "text": tail], event: "chunk")
        }
        if cancelled {
            settle(job, error: nil, aborted: true)
            return
        }
        if let error = error as NSError? {
            if error.code == NSURLErrorCancelled {
                settle(job, error: nil, aborted: true)
            } else if error.code == NSURLErrorTimedOut {
                settle(job, error: "timeout", aborted: false)
            } else {
                settle(job, error: "network", aborted: false)
            }
            return
        }
        settle(job, error: nil, aborted: false)
    }

    private func job(for task: URLSessionTask) -> Job? {
        guard let id = task.taskDescription, !id.isEmpty else { return nil }
        jobsLock.lock()
        defer { jobsLock.unlock() }
        return jobs[id]
    }

    private func emit(_ data: [String: Any], event: String) {
        DispatchQueue.main.async { [weak self] in
            self?.notifyListeners(event, data: data)
        }
    }

    private func settle(_ job: Job, error: String?, aborted: Bool) {
        jobsLock.lock()
        if job.settled {
            jobsLock.unlock()
            return
        }
        job.settled = true
        jobs.removeValue(forKey: job.id)
        let status = job.status
        let session = job.session
        jobsLock.unlock()
        DispatchQueue.main.async {
            if let error {
                job.call.reject(error)
            } else {
                job.call.resolve(["status": status, "aborted": aborted])
            }
        }
        session.finishTasksAndInvalidate()
    }
}

func takeUTF8(_ pending: inout Data, end: Bool) -> String {
    if pending.isEmpty { return "" }
    let cut = end ? pending.count : completeUTF8End(pending)
    if cut <= 0 { return "" }
    let head = pending.prefix(cut)
    pending.removeFirst(cut)
    return String(data: head, encoding: .utf8) ?? ""
}

func completeUTF8End(_ data: Data) -> Int {
    let n = data.count
    if n == 0 { return 0 }
    var i = n - 1
    var cont = 0
    while i >= 0 && data[i] & 0b1100_0000 == 0b1000_0000 {
        cont += 1
        i -= 1
        if cont > 3 { return n }
    }
    if i < 0 { return 0 }
    let lead = data[i]
    let need: Int
    if lead & 0b1000_0000 == 0 {
        need = 1
    } else if lead & 0b1110_0000 == 0b1100_0000 {
        need = 2
    } else if lead & 0b1111_0000 == 0b1110_0000 {
        need = 3
    } else if lead & 0b1111_1000 == 0b1111_0000 {
        need = 4
    } else {
        return n
    }
    if n - i < need { return i }
    return n
}
