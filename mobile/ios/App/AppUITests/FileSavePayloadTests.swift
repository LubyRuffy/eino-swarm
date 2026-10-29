import Foundation
import XCTest

/// The App and this target compile the same FileSavePayload.swift source.
/// These checks do not stand in for the real document-picker export below.
final class FileSavePayloadTests: XCTestCase {
    private func decode(_ bytes: Data, name: String = "report.md") throws -> FileSavePayload {
        try FileSavePayload(name: name, mime: "application/octet-stream",
                            data: bytes.base64EncodedString(), size: NSNumber(value: bytes.count))
    }

    func testEmptyBinaryBOMAndUnicodeBytesAreNotTranscoded() throws {
        for original in [Data(), Data([0, 0xff, 0x80, 0x0d, 0x0a]),
                         Data([0xef, 0xbb, 0xbf]) + Data("中文\r\n尾空格  ".utf8)] {
            let payload = try decode(original, name: " 报告 café .md ")
            XCTAssertEqual(payload.bytes, original)
            XCTAssertEqual(Data(payload.name.utf8), Data(" 报告 café .md ".utf8))
        }
    }

    func testSizeMustBeAnExactIntegerByteCount() {
        let invalid: [Any] = [true, false, "1", NSNumber(value: -1), NSNumber(value: 1.5),
                              NSNumber(value: Double.nan), NSNumber(value: Double.infinity),
                              NSNumber(value: FileSavePayload.maximumBytes + 1)]
        for size in invalid {
            XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain", data: "YQ==", size: size))
        }
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain", data: "YQ==", size: 0))
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain", data: "", size: 1))
    }

    func testBase64IsCanonicalAndCannotIgnoreExtraCharacters() {
        for data in ["YQ", "YQ=", "YR==", "YQ==\n", "YQ== ", "!!", "YQ==="] {
            XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain", data: data, size: 1), data)
        }
    }

    func testBasenameCannotSelectAnotherPathAndIsNotTrimmed() throws {
        for name in ["", ".", "..", "/a", "a/b", "a\\b", "a\0b", "a\nb", String(repeating: "a", count: 256)] {
            XCTAssertThrowsError(try decode(Data(), name: name), name)
        }
        XCTAssertEqual(try decode(Data(), name: " .hidden ").name, " .hidden ")
    }

    func testMissingOrUnsafeMetadataIsRejected() {
        XCTAssertThrowsError(try FileSavePayload(name: nil, mime: "text/plain", data: "", size: 0))
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: nil, data: "", size: 0))
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "", data: "", size: 0))
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain\r\nx", data: "", size: 0))
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain", data: nil, size: 0))
        XCTAssertThrowsError(try FileSavePayload(name: "a", mime: "text/plain", data: "", size: nil))
    }

    func testMaximumRawFileIsAcceptedWithoutChangingItsBytes() throws {
        let bytes = Data(repeating: 0x9f, count: FileSavePayload.maximumBytes)
        XCTAssertEqual(try decode(bytes).bytes, bytes)
    }
}
