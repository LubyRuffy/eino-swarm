import CoreFoundation
import Foundation

enum FileSavePayloadError: Error { case invalidFile }

/// Shared with the native test target; content is never passed through String.
struct FileSavePayload {
    static let maximumBytes = 32 * 1024 * 1024
    let name: String
    let mime: String
    let bytes: Data

    init(name: String?, mime: String?, data: String?, size: Any?) throws {
        guard let name, !name.isEmpty, name != ".", name != "..",
              name.utf8.count <= 255, !name.contains("/"), !name.contains("\\"),
              name.rangeOfCharacter(from: .controlCharacters) == nil,
              let mime, !mime.isEmpty, mime.utf8.count <= 255,
              mime.rangeOfCharacter(from: .controlCharacters) == nil,
              let number = size as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID(),
              number.doubleValue.isFinite,
              number.doubleValue >= 0, number.doubleValue <= Double(Self.maximumBytes),
              number.doubleValue.rounded(.towardZero) == number.doubleValue,
              let data, data.utf8.count <= ((Self.maximumBytes + 2) / 3) * 4,
              let bytes = Data(base64Encoded: data),
              bytes.count == Int(number.doubleValue), bytes.count <= Self.maximumBytes,
              bytes.base64EncodedString() == data else {
            throw FileSavePayloadError.invalidFile
        }
        self.name = name
        self.mime = mime
        self.bytes = bytes
    }
}
