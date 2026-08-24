import AppKit
import Foundation

enum VitalsFormatters {
    static func bytes(_ value: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: value, countStyle: .file)
    }

    static func duration(milliseconds: Int64) -> String {
        let seconds = Double(milliseconds) / 1_000
        if seconds < 60 {
            return String(format: "%.2f seconds", seconds)
        }
        let minutes = Int(seconds) / 60
        let remainder = Int(seconds) % 60
        return "\(minutes)m \(remainder)s"
    }

    static func yesNo(_ value: Bool) -> String {
        value ? "Yes" : "No"
    }
}

enum ClipboardService {
    @MainActor
    @discardableResult
    static func copy(_ value: String) -> Bool {
        NSPasteboard.general.clearContents()
        return NSPasteboard.general.setString(value, forType: .string)
    }
}
