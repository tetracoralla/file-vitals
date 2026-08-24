import Foundation

struct InspectionResult: Decodable, Sendable {
    let schemaVersion: String
    let status: String
    let file: InspectedFile
    let identity: FileIdentity
    let traits: [String]
    let integrity: FileIntegrity
    let text: TextFacts?
    let structured: StructuredFacts?
    let image: ImageFacts?
    let media: MediaFacts?
    let videoStreams: [VideoStreamFacts]?
    let audioStreams: [AudioStreamFacts]?
    let archive: ArchiveFacts?
    let pdf: PDFFacts?
    let font: FontFacts?
    let binary: BinaryFacts?
    let diagnostics: [InspectionDiagnostic]
    let error: InspectionError?
}

struct InspectedFile: Decodable, Sendable {
    let name: String
    let sizeBytes: Int64
    let `extension`: String
    let modifiedUtc: String?
}

struct FileIdentity: Decodable, Sendable {
    let kind: String
    let mediaType: String
    let format: String
    let formatVersion: String?
    let confidence: String
    let extensionMatch: Bool?
    let conflicts: [String]
}

struct FileIntegrity: Decodable, Sendable {
    let readable: Bool
    let parseable: Bool?
    let sha256: String?
}

struct TextFacts: Decodable, Sendable {
    let encoding: EncodingFacts
    let bom: String?
    let lineCount: Int64?
    let lineEnding: String?
    let finalNewline: Bool?
    let sampled: Bool
    let sampleBytes: Int
}

struct EncodingFacts: Decodable, Sendable {
    let value: String
    let certainty: String
    let evidence: [String]
}

struct StructuredFacts: Decodable, Sendable {
    let format: String
    let parseable: Bool?
}

struct ImageFacts: Decodable, Sendable {
    let width: Int
    let height: Int
    let bitDepth: Int?
    let colorModel: String?
    let hasAlpha: Bool?
    let frameCount: Int?
}

struct MediaFacts: Decodable, Sendable {
    let durationMs: Int64?
    let bitrateBps: Int64?
    let container: String?
}

struct VideoStreamFacts: Decodable, Sendable {
    let index: Int
    let codec: String
    let profile: String?
    let width: Int?
    let height: Int?
    let fps: RationalFacts?
    let bitrateBps: Int64?
    let pixelFormat: String?
}

struct AudioStreamFacts: Decodable, Sendable {
    let index: Int
    let codec: String
    let sampleRateHz: Int?
    let channels: Int?
    let channelLayout: String?
    let bitrateBps: Int64?
}

struct RationalFacts: Decodable, Sendable {
    let num: Int64
    let den: Int64
}

struct ArchiveFacts: Decodable, Sendable {
    let format: String
    let entryCount: Int?
    let entriesScanned: Int
    let totalUncompressedBytes: Int64?
    let uncompressedBytesScanned: Int64
    let encrypted: Bool
    let entries: [ArchiveEntryFacts]?
    let entriesTruncated: Bool
    let scanTruncated: Bool
}

struct ArchiveEntryFacts: Decodable, Sendable {
    let name: String
    let sizeBytes: Int64
    let compressedBytes: Int64?
    let directory: Bool
    let encrypted: Bool
}

struct PDFFacts: Decodable, Sendable {
    let version: String?
    let pageCount: Int?
    let encrypted: Bool?
    let title: String?
    let author: String?
}

struct FontFacts: Decodable, Sendable {
    let format: String
    let family: String?
    let subfamily: String?
    let weight: Int?
    let variable: Bool
    let glyphCount: Int?
    let axes: [FontAxisFacts]?
}

struct FontAxisFacts: Decodable, Sendable {
    let tag: String
    let minimum: Double
    let `default`: Double
    let maximum: Double
}

struct BinaryFacts: Decodable, Sendable {
    let format: String
    let architectures: [String]?
    let bits: Int?
    let endianness: String?
}

struct InspectionDiagnostic: Decodable, Sendable {
    let code: String
    let severity: String
    let message: String
}

struct InspectionError: Decodable, Sendable {
    let code: String
    let message: String
}

struct InspectionPayload: Sendable {
    let result: InspectionResult
    let json: String
}
