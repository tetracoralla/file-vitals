import Foundation

enum SummaryFormatter {
    static func summary(for result: InspectionResult) -> String {
        let format = result.identity.format.isEmpty ? "Unknown" : result.identity.format
        var lines = [
            "\(format) · \(result.identity.mediaType) · \(result.identity.confidence) · \(result.status)",
            "\(result.file.name) · \(VitalsFormatters.bytes(result.file.sizeBytes))",
        ]
        if let image = result.image {
            lines.append("\(image.width) × \(image.height)" + optionalSuffix(image.colorModel))
        }
        if let text = result.text {
            var value = "\(text.encoding.value) \(text.encoding.certainty)"
            if let count = text.lineCount {
                value += " · \(count) lines"
            }
            lines.append(value)
        }
        if let archive = result.archive {
            var value = "\(archive.entriesScanned) entries scanned"
            if let total = archive.entryCount {
                value += " / \(total) total"
            }
            lines.append(value)
        }
        if let digest = result.integrity.sha256, !digest.isEmpty {
            lines.append("SHA-256: \(digest)")
        }
        if !result.constraints.isEmpty {
            lines.append("Action blockers: " + result.constraints.map(constraintLabel).joined(separator: ", "))
        }
        for diagnostic in result.diagnostics {
            lines.append("\(diagnostic.severity.uppercased()) \(diagnostic.code): \(diagnostic.message)")
        }
        if let error = result.error {
            lines.append("\(error.code): \(error.message)")
        }
        return lines.joined(separator: "\n")
    }

    private static func optionalSuffix(_ value: String?) -> String {
        guard let value, !value.isEmpty else { return "" }
        return " · \(value)"
    }

    static func constraintLabel(_ value: String) -> String {
        switch value {
        case "active_content": "contains active content"
        case "archive_devices": "archive contains device or pipe entries"
        case "archive_links": "archive contains link entries"
        case "archive_unsafe_paths": "archive contains unsafe paths"
        case "embedded_objects": "contains embedded objects"
        case "encrypted": "content is encrypted"
        case "external_references": "contains external references"
        case "indirect_content": "file is an indirection pointer"
        case "integrity_mismatch": "SHA-256 does not match the expected digest"
        default: "action is constrained"
        }
    }
}
