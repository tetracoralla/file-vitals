import Foundation
import XCTest
@testable import FileVitalsApp

final class DropSelectionTests: XCTestCase {
    func testAcceptsFirstLocalFileURL() {
        let url = URL(fileURLWithPath: "/tmp/file-vitals-drop.txt")

        XCTAssertEqual(DropSelection.firstFileURL(in: [url]), url)
    }

    func testRejectsEmptyDrop() {
        XCTAssertNil(DropSelection.firstFileURL(in: []))
    }

    func testRejectsRemoteURL() {
        let url = URL(string: "https://example.com/file.txt")!

        XCTAssertNil(DropSelection.firstFileURL(in: [url]))
    }

    func testDoesNotSkipInvalidFirstItem() {
        let remote = URL(string: "https://example.com/file.txt")!
        let local = URL(fileURLWithPath: "/tmp/file-vitals-drop.txt")

        XCTAssertNil(DropSelection.firstFileURL(in: [remote, local]))
    }
}
