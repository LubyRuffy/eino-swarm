import Capacitor
import Darwin
import Foundation
import UIKit

/// Exports the bytes already received over the paired PC link. A WebView blob
/// URL is not a file that another application or the Files provider can read.
@objc(FileSavePlugin)
public class FileSavePlugin: CAPPlugin, CAPBridgedPlugin, UIDocumentPickerDelegate, UIAdaptivePresentationControllerDelegate {
    public let identifier = "FileSavePlugin"
    public let jsName = "FileSave"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "saveFile", returnType: CAPPluginReturnPromise),
    ]

    private let stateLock = NSLock()
    private let ioQueue = DispatchQueue(label: "com.lubyruffy.zwai.file-export", qos: .userInitiated)
    private var pending: Export?
    private var destroyed = false

    private final class Export {
        let call: CAPPluginCall
        let directory: URL
        // Only read or written on the main queue.
        var picker: UIDocumentPickerViewController?
        // Protected by stateLock, including the first completion claim.
        var completing = false
        // Only the serial I/O queue owns this flag. A failed mkdir must never
        // grant permission to delete a preexisting path, however unlikely.
        var directoryCreated = false

        init(_ call: CAPPluginCall) {
            self.call = call
            directory = FileManager.default.temporaryDirectory.appendingPathComponent(
                "zwai-file-export-" + UUID().uuidString, isDirectory: true
            )
        }
    }

    @objc func saveFile(_ call: CAPPluginCall) {
        stateLock.lock()
        if destroyed || pending != nil {
            let code = destroyed ? "unavailable" : "busy"
            stateLock.unlock()
            call.reject("File export is unavailable or already pending.", code)
            return
        }
        let request = Export(call)
        pending = request
        stateLock.unlock()

        // Decoding, writing, readback and deletion do not run on UIKit's queue.
        ioQueue.async { [weak self] in
            guard let self else { return }
            do {
                let file = try self.prepare(request)
                DispatchQueue.main.async { [weak self] in self?.present(request, file: file) }
            } catch FileSavePayloadError.invalidFile {
                self.finish(request, saved: false, error: "invalid_file")
            } catch {
                self.finish(request, saved: false, error: "save_failed")
            }
        }
    }

    private enum ExportError: Error { case writeFailed }

    private func prepare(_ request: Export) throws -> URL {
        let call = request.call
        let payload = try FileSavePayload(
            name: call.getString("name"), mime: call.getString("mime"),
            data: call.getString("data"), size: call.options["size"]
        )
        let name = payload.name, bytes = payload.bytes
        // Never trim or decode/re-encode file content or the supplied basename.
        let file = request.directory.appendingPathComponent(name, isDirectory: false)
        guard file.deletingLastPathComponent() == request.directory,
              file.lastPathComponent == name else { throw FileSavePayloadError.invalidFile }
        try FileManager.default.createDirectory(
            at: request.directory, withIntermediateDirectories: false,
            attributes: [.posixPermissions: 0o700]
        )
        request.directoryCreated = true
        let descriptor = Darwin.open(file.path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, mode_t(0o600))
        guard descriptor >= 0 else { throw ExportError.writeFailed }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        do {
            try handle.write(contentsOf: bytes)
            try handle.synchronize()
            try handle.close()
        } catch {
            try? handle.close()
            throw ExportError.writeFailed
        }
        guard try Data(contentsOf: file, options: .mappedIfSafe) == bytes else {
            throw ExportError.writeFailed
        }
        return file
    }

    private func present(_ request: Export, file: URL) {
        stateLock.lock()
        let current = pending === request && !request.completing && !destroyed
        stateLock.unlock()
        guard current else { return }
        guard UIApplication.shared.applicationState == .active,
              let presenter = bridge?.viewController,
              presenter.viewIfLoaded?.window != nil,
              presenter.presentedViewController == nil,
              !presenter.isBeingDismissed else {
            finish(request, saved: false, error: "unavailable")
            return
        }
        let picker = UIDocumentPickerViewController(forExporting: [file], asCopy: true)
        picker.allowsMultipleSelection = false
        picker.delegate = self
        request.picker = picker
        picker.presentationController?.delegate = self
        presenter.present(picker, animated: true) { [weak self] in
            picker.presentationController?.delegate = self
            if picker.presentingViewController == nil {
                self?.finish(request, saved: false, error: "unavailable")
            }
        }
    }

    public func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        guard let request = request(for: controller) else { return }
        guard urls.count == 1, urls[0].isFileURL else {
            finish(request, saved: false, error: "save_failed")
            return
        }
        // This delegate is the system's completed export, not its presentation.
        finish(request, saved: true)
    }

    public func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
        if let request = request(for: controller) { finish(request, saved: false) }
    }

    public func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard let picker = presentationController.presentedViewController as? UIDocumentPickerViewController,
              let request = request(for: picker) else { return }
        finish(request, saved: false)
    }

    private func request(for picker: UIDocumentPickerViewController) -> Export? {
        stateLock.lock()
        defer { stateLock.unlock() }
        guard let request = pending, request.picker === picker, !request.completing else { return nil }
        return request
    }

    private func finish(_ request: Export, saved: Bool, error: String? = nil) {
        stateLock.lock()
        guard pending === request, !request.completing else {
            stateLock.unlock()
            return
        }
        request.completing = true
        stateLock.unlock()
        DispatchQueue.main.async {
            request.picker?.delegate = nil
            request.picker?.presentationController?.delegate = nil
            request.picker?.dismiss(animated: true)
            self.ioQueue.async {
                var failure = error
                do {
                    try Self.removeTemporary(request)
                } catch { failure = "cleanup_failed" }
                DispatchQueue.main.async {
                    self.stateLock.lock()
                    self.pending = nil
                    let ended = self.destroyed
                    self.stateLock.unlock()
                    if let code = failure ?? (ended ? "unavailable" : nil) {
                        request.call.reject("File export could not be completed.", code)
                    } else {
                        request.call.resolve(["saved": saved])
                    }
                }
            }
        }
    }

    /// Called by the owning bridge controller before its stored plugin releases.
    func cancelForDestroy() {
        stateLock.lock()
        destroyed = true
        let request = pending
        stateLock.unlock()
        if let request { finish(request, saved: false, error: "unavailable") }
    }

    private static func removeTemporary(_ request: Export) throws {
        guard request.directoryCreated else { return }
        if FileManager.default.fileExists(atPath: request.directory.path) {
            try FileManager.default.removeItem(at: request.directory)
        }
        request.directoryCreated = false
    }

    deinit {
        // Fallback for disposal outside the owning controller. Queue ordering
        // keeps deletion after any already admitted write to this request only.
        if let request = pending {
            ioQueue.async {
                try? Self.removeTemporary(request)
                DispatchQueue.main.async {
                    request.picker?.delegate = nil
                    request.picker?.dismiss(animated: false)
                    request.call.reject("File export ended.", "unavailable")
                }
            }
        }
    }
}
