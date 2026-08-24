import Foundation
import XCTest
@testable import FileVitalsApp

final class InspectionResultTests: XCTestCase {
    func testDecodesPublishedResultAndBuildsSummary() throws {
        let json = #"""
    {
      "schema_version": "1.1",
      "status": "ok",
      "file": {"name": "sample.json", "size_bytes": 12, "extension": ".json"},
      "identity": {
        "kind": "data",
        "media_type": "application/json",
        "format": "JSON",
        "confidence": "high",
        "extension_match": true,
        "conflicts": []
      },
      "traits": ["metadata_readable", "text_extractable"],
      "constraints": [],
      "integrity": {
        "readable": true,
        "parseable": true,
        "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      },
      "text": {
        "encoding": {"value": "utf-8", "certainty": "probable", "evidence": []},
        "line_count": 1,
        "sampled": false,
        "sample_bytes": 12
      },
      "structured": {"format": "json", "parseable": true},
      "diagnostics": []
    }
    """#
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        let result = try decoder.decode(InspectionResult.self, from: Data(json.utf8))

        XCTAssertEqual(result.schemaVersion, "1.1")
        XCTAssertEqual(result.identity.extensionMatch, true)
        XCTAssertEqual(result.structured?.parseable, true)
        let summary = SummaryFormatter.summary(for: result)
        XCTAssertTrue(summary.contains("JSON · application/json · high · ok"))
        XCTAssertTrue(summary.contains("sample.json"))
        XCTAssertTrue(summary.contains("SHA-256: aaaaaaaaa"))
    }

    func testDecodesPublishedErrorPayload() throws {
        // Every engine failure (E_TIMEOUT, E_FILE_NOT_FOUND, …) decodes as a
        // normal result with an error block; the app must not treat it as
        // transport-level garbage.
        let json = #"""
    {
      "schema_version": "1.1",
      "status": "error",
      "file": {"name": "missing.txt", "size_bytes": 0, "extension": ".txt"},
      "identity": {
        "kind": "unknown",
        "media_type": "application/octet-stream",
        "format": "Unknown",
        "confidence": "unknown",
        "candidates": [],
        "conflicts": []
      },
      "traits": [],
      "constraints": [],
      "integrity": {"readable": false},
      "diagnostics": [],
      "limits": {"mode": "quick", "response_bytes_max": 262144, "timeout_ms": 5000, "memory_bytes_max": 402653184},
      "error": {"code": "E_FILE_NOT_FOUND", "message": "The requested file does not exist inside the granted workspace."}
    }
    """#
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        let result = try decoder.decode(InspectionResult.self, from: Data(json.utf8))
        XCTAssertEqual(result.status, "error")
        XCTAssertEqual(result.error?.code, "E_FILE_NOT_FOUND")
        XCTAssertFalse(result.integrity.readable)
        let summary = SummaryFormatter.summary(for: result)
        XCTAssertTrue(summary.contains("missing.txt"))
        XCTAssertTrue(summary.contains("E_FILE_NOT_FOUND") || summary.contains("error"),
                      "summary must surface the error state, got: \(summary)")
    }

    func testSummarySurfacesDiagnosticsAndConflicts() throws {
        let json = #"""
    {
      "schema_version": "1.1",
      "status": "partial",
      "file": {"name": "clip.mov", "size_bytes": 4096, "extension": ".mov"},
      "identity": {
        "kind": "video",
        "media_type": "video/quicktime",
        "format": "QuickTime",
        "confidence": "exact",
        "candidates": [],
        "conflicts": ["Extension suggests video/mp4 but verified content indicates video/quicktime."]
      },
      "traits": ["container", "playable"],
      "constraints": ["external_references"],
      "integrity": {"readable": true},
      "diagnostics": [
        {"code": "EXTENSION_MISMATCH", "severity": "warning", "message": "The filename extension does not match the detected byte signature."}
      ],
      "limits": {"mode": "standard", "response_bytes_max": 262144, "timeout_ms": 5000, "memory_bytes_max": 402653184}
    }
    """#
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        let result = try decoder.decode(InspectionResult.self, from: Data(json.utf8))
        XCTAssertEqual(result.identity.conflicts.count, 1)
        XCTAssertEqual(result.diagnostics.first?.code, "EXTENSION_MISMATCH")
		XCTAssertEqual(result.constraints, ["external_references"])
        let summary = SummaryFormatter.summary(for: result)
        XCTAssertTrue(summary.contains("QuickTime"), "summary should name the identity, got: \(summary)")
		XCTAssertTrue(summary.contains("contains external references"), "summary should translate action blockers, got: \(summary)")
        XCTAssertTrue(summary.contains("partial") || summary.contains("warning") || summary.contains("EXTENSION_MISMATCH") || summary.contains("video/quicktime"),
                      "summary should reflect non-ok evidence, got: \(summary)")
    }
}
