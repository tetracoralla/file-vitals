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
}
