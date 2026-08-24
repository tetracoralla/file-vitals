import SwiftUI

struct InspectionView: View {
    let result: InspectionResult
    let sourceURL: URL
    let rawJSON: String
    @State private var copiedAction: CopyAction?
    @State private var copyRevision = UUID()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                header
                overview
                diagnostics
                familyFacts
                integrity
                copyActions
            }
            .padding(24)
            .frame(maxWidth: 900, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: iconName)
                .font(.system(size: 32))
                .foregroundStyle(.secondary)
                .frame(width: 42, height: 42)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(result.file.name)
                    .font(.title2.weight(.semibold))
                    .textSelection(.enabled)
                Text(sourceURL.deletingLastPathComponent().path)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
            }
            Spacer()
            StatusBadge(status: result.status)
        }
    }

    private var overview: some View {
        FactSection("Overview") {
            FactRow(label: "Format", value: result.identity.format.isEmpty ? "Unknown" : result.identity.format)
            FactRow(label: "Media type", value: result.identity.mediaType)
            FactRow(label: "Kind", value: result.identity.kind)
            FactRow(label: "Confidence", value: result.identity.confidence.capitalized)
            FactRow(label: "Size", value: VitalsFormatters.bytes(result.file.sizeBytes))
            if !result.file.extension.isEmpty {
                FactRow(label: "Extension", value: result.file.extension)
            }
            if let matches = result.identity.extensionMatch {
                FactRow(label: "Extension matches", value: VitalsFormatters.yesNo(matches))
            }
            if let modified = result.file.modifiedUtc, !modified.isEmpty {
                FactRow(label: "Modified", value: modified)
            }
        }
    }

    @ViewBuilder
    private var diagnostics: some View {
        if !result.constraints.isEmpty || !result.identity.conflicts.isEmpty || !result.diagnostics.isEmpty || result.error != nil {
            FactSection(result.error == nil ? "Warnings" : "Error") {
                ForEach(result.constraints, id: \.self) { constraint in
                    diagnosticRow(icon: "exclamationmark.shield.fill", message: SummaryFormatter.constraintLabel(constraint), color: .orange)
                }
                ForEach(Array(result.identity.conflicts.enumerated()), id: \.offset) { _, conflict in
                    diagnosticRow(icon: "exclamationmark.triangle.fill", message: conflict, color: .orange)
                }
                ForEach(Array(result.diagnostics.enumerated()), id: \.offset) { _, diagnostic in
                    diagnosticRow(
                        icon: diagnostic.severity == "error" ? "xmark.octagon.fill" : "exclamationmark.triangle.fill",
                        message: diagnostic.message,
                        color: diagnostic.severity == "error" ? .red : .orange
                    )
                }
                if let error = result.error {
                    diagnosticRow(icon: "xmark.octagon.fill", message: error.message, color: .red)
                }
            }
        }
    }

    @ViewBuilder
    private var familyFacts: some View {
        if let text = result.text {
            FactSection("Text") {
                FactRow(label: "Encoding", value: "\(text.encoding.value) (\(text.encoding.certainty))")
                if let lines = text.lineCount { FactRow(label: "Lines", value: "\(lines)") }
                if let ending = text.lineEnding, !ending.isEmpty { FactRow(label: "Line endings", value: ending.uppercased()) }
                if let finalNewline = text.finalNewline { FactRow(label: "Final newline", value: VitalsFormatters.yesNo(finalNewline)) }
                if let bom = text.bom, !bom.isEmpty { FactRow(label: "Byte-order mark", value: bom) }
            }
        }
        if let structured = result.structured {
            FactSection("Structure") {
                FactRow(label: "Syntax", value: structured.format.uppercased())
                FactRow(label: "Parseable", value: structured.parseable.map(VitalsFormatters.yesNo) ?? "Not determined")
            }
        }
        if let image = result.image {
            FactSection("Image") {
                FactRow(label: "Dimensions", value: "\(image.width) × \(image.height)")
                if let colorModel = image.colorModel, !colorModel.isEmpty { FactRow(label: "Color model", value: colorModel) }
                if let bitDepth = image.bitDepth, bitDepth > 0 { FactRow(label: "Bit depth", value: "\(bitDepth)") }
                if let hasAlpha = image.hasAlpha { FactRow(label: "Alpha", value: VitalsFormatters.yesNo(hasAlpha)) }
                if let frames = image.frameCount, frames > 0 { FactRow(label: "Frames", value: "\(frames)") }
            }
        }
        if result.media != nil || !(result.videoStreams ?? []).isEmpty || !(result.audioStreams ?? []).isEmpty {
            mediaFacts
        }
        if let archive = result.archive {
            archiveFacts(archive)
        }
        if let pdf = result.pdf {
            FactSection("PDF") {
                if let version = pdf.version, !version.isEmpty { FactRow(label: "Version", value: version) }
                if let pages = pdf.pageCount, pages > 0 { FactRow(label: "Pages", value: "\(pages)") }
                if let encrypted = pdf.encrypted { FactRow(label: "Encrypted", value: VitalsFormatters.yesNo(encrypted)) }
                if let title = pdf.title, !title.isEmpty { FactRow(label: "Title", value: title) }
                if let author = pdf.author, !author.isEmpty { FactRow(label: "Author", value: author) }
                FactRow(label: "Text layer", value: pdf.textLayer.capitalized)
                if pdf.textPagesSampled > 0 { FactRow(label: "Text pages sampled", value: "\(pdf.textPagesSampled)") }
                FactRow(label: "Text scan complete", value: VitalsFormatters.yesNo(pdf.textLayerComplete))
            }
        }
        if let ooxml = result.ooxml {
            FactSection("Office package") {
                FactRow(label: "Kind", value: ooxml.kind.uppercased())
                if let sheets = ooxml.sheetCount { FactRow(label: "Sheets", value: "\(sheets)") }
                if let slides = ooxml.slideCount { FactRow(label: "Slides", value: "\(slides)") }
                FactRow(label: "Macros", value: VitalsFormatters.yesNo(ooxml.macroEnabled))
                FactRow(label: "External references", value: "\(ooxml.externalRelationships)")
                FactRow(label: "Embedded objects", value: "\(ooxml.embeddedObjects)")
            }
        }
        if let svg = result.svg {
            FactSection("SVG") {
                FactRow(label: "Scripts", value: "\(svg.scriptCount)")
                FactRow(label: "External references", value: "\(svg.externalHrefCount)")
            }
        }
        if let indirection = result.indirection {
            FactSection("Indirection") {
                FactRow(label: "Type", value: indirection.kind == "git_lfs_pointer" ? "Git LFS pointer" : "Pointer")
                FactRow(label: "Object", value: indirection.oid)
                FactRow(label: "Declared size", value: VitalsFormatters.bytes(indirection.declaredSize))
            }
        }
        if let font = result.font {
            FactSection("Font") {
                FactRow(label: "Format", value: font.format)
                if let family = font.family, !family.isEmpty { FactRow(label: "Family", value: family) }
                if let subfamily = font.subfamily, !subfamily.isEmpty { FactRow(label: "Style", value: subfamily) }
                if let weight = font.weight, weight > 0 { FactRow(label: "Weight", value: "\(weight)") }
                if let glyphs = font.glyphCount, glyphs > 0 { FactRow(label: "Glyphs", value: "\(glyphs)") }
                FactRow(label: "Variable", value: VitalsFormatters.yesNo(font.variable))
            }
        }
        if let binary = result.binary {
            FactSection("Binary") {
                FactRow(label: "Format", value: binary.format)
                if let architectures = binary.architectures, !architectures.isEmpty { FactRow(label: "Architecture", value: architectures.joined(separator: ", ")) }
                if let bits = binary.bits, bits > 0 { FactRow(label: "Word size", value: "\(bits)-bit") }
                if let endianness = binary.endianness, !endianness.isEmpty { FactRow(label: "Endianness", value: endianness.capitalized) }
            }
        }
    }

    @ViewBuilder
    private var mediaFacts: some View {
        FactSection("Media") {
            if let media = result.media {
                if let duration = media.durationMs { FactRow(label: "Duration", value: VitalsFormatters.duration(milliseconds: duration)) }
                if let container = media.container, !container.isEmpty { FactRow(label: "Container", value: container) }
                if let bitrate = media.bitrateBps, bitrate > 0 { FactRow(label: "Bitrate", value: "\(bitrate) bps") }
            }
            ForEach(result.videoStreams ?? [], id: \.index) { stream in
                FactRow(label: "Video \(stream.index)", value: videoSummary(stream))
            }
            ForEach(result.audioStreams ?? [], id: \.index) { stream in
                FactRow(label: "Audio \(stream.index)", value: audioSummary(stream))
            }
        }
    }

    private func archiveFacts(_ archive: ArchiveFacts) -> some View {
        FactSection("Archive") {
            FactRow(label: "Format", value: archive.format)
            FactRow(label: "Entries scanned", value: "\(archive.entriesScanned)")
            if let count = archive.entryCount { FactRow(label: "Total entries", value: "\(count)") }
            if let total = archive.totalUncompressedBytes { FactRow(label: "Uncompressed", value: VitalsFormatters.bytes(total)) }
            FactRow(label: "Encrypted", value: VitalsFormatters.yesNo(archive.encrypted))
            if archive.pathFacts.absolutePaths > 0 { FactRow(label: "Absolute paths", value: "\(archive.pathFacts.absolutePaths)") }
            if archive.pathFacts.parentPaths > 0 { FactRow(label: "Parent paths", value: "\(archive.pathFacts.parentPaths)") }
            if archive.pathFacts.linkEntries > 0 { FactRow(label: "Link entries", value: "\(archive.pathFacts.linkEntries)") }
            if archive.pathFacts.deviceEntries > 0 { FactRow(label: "Device or pipe entries", value: "\(archive.pathFacts.deviceEntries)") }
            ForEach(Array((archive.entries ?? []).enumerated()), id: \.offset) { _, entry in
                HStack(alignment: .firstTextBaseline) {
                    Image(systemName: entry.directory ? "folder" : "doc")
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                    Text(entry.name)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .textSelection(.enabled)
                    Spacer()
                    Text(VitalsFormatters.bytes(entry.sizeBytes))
                        .foregroundStyle(.secondary)
                }
            }
            if archive.entriesTruncated {
                Text("Additional entries omitted")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private var integrity: some View {
        if let digest = result.integrity.sha256, !digest.isEmpty {
            FactSection("Integrity") {
                FactRow(label: "SHA-256", value: digest)
                if let expected = result.integrity.expectedSha256 { FactRow(label: "Expected", value: expected) }
                if let matches = result.integrity.sha256Matches { FactRow(label: "Matches expected", value: VitalsFormatters.yesNo(matches)) }
            }
        }
    }

    private var copyActions: some View {
        HStack {
            Button {
                copy(SummaryFormatter.summary(for: result), action: .summary)
            } label: {
                Label(copiedAction == .summary ? "Copied" : "Copy Summary", systemImage: copiedAction == .summary ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut("c", modifiers: [.command, .shift])

            Button {
                copy(rawJSON, action: .json)
            } label: {
                Label(copiedAction == .json ? "Copied" : "Copy JSON", systemImage: copiedAction == .json ? "checkmark" : "curlybraces")
            }
        }
        .padding(.bottom, 8)
    }

    private func diagnosticRow(icon: String, message: String, color: Color) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: icon)
                .foregroundStyle(color)
                .accessibilityHidden(true)
            Text(message)
                .textSelection(.enabled)
        }
    }

    private func videoSummary(_ stream: VideoStreamFacts) -> String {
        var values = [stream.codec]
        if let width = stream.width, let height = stream.height, width > 0, height > 0 {
            values.append("\(width) × \(height)")
        }
        if let fps = stream.fps, fps.den != 0 {
            values.append("\(fps.num)/\(fps.den) fps")
        }
        return values.joined(separator: " · ")
    }

    private func audioSummary(_ stream: AudioStreamFacts) -> String {
        var values = [stream.codec]
        if let rate = stream.sampleRateHz, rate > 0 { values.append("\(rate) Hz") }
        if let channels = stream.channels, channels > 0 { values.append("\(channels) channels") }
        return values.joined(separator: " · ")
    }

    private var iconName: String {
        switch result.identity.kind {
        case "image": "photo"
        case "audio": "waveform"
        case "video", "media": "film"
        case "archive": "archivebox"
        case "document": "doc.richtext"
        case "font": "textformat"
        case "binary": "terminal"
        case "data": "tablecells"
        case "text": "doc.text"
        default: "doc"
        }
    }

    private func copy(_ value: String, action: CopyAction) {
        // A failed pasteboard write must not show the copied confirmation.
        guard ClipboardService.copy(value) else { return }
        let revision = UUID()
        copyRevision = revision
        copiedAction = action
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.5))
            if copyRevision == revision {
                copiedAction = nil
            }
        }
    }
}

private enum CopyAction {
    case summary
    case json
}
