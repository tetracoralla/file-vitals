import Foundation
import Observation

@MainActor
@Observable
final class InspectionStore {
    var selectedURL: URL?
    var mode: InspectionMode = .standard
    var includeSHA256 = false
    var result: InspectionResult?
    var rawJSON = ""
    var isInspecting = false
    var failureMessage: String?

    private let client = InspectorClient()
    private var inspectionTask: Task<Void, Never>?
    private var revision = UUID()

    func select(_ url: URL) {
        selectedURL = url
        inspect()
    }

    func inspect() {
        guard let url = selectedURL else { return }
        inspectionTask?.cancel()
        let currentRevision = UUID()
        revision = currentRevision
        isInspecting = true
        result = nil
        rawJSON = ""
        failureMessage = nil
        let mode = mode
        let includeSHA256 = includeSHA256

        inspectionTask = Task {
            // The selection is captured before the task so a cancelled-but-
            // pending older task can never inspect a newer selection twice.
            let scoped = url.startAccessingSecurityScopedResource()
            defer {
                if scoped {
                    url.stopAccessingSecurityScopedResource()
                }
            }
            do {
                let payload = try await client.inspect(
                    url: url,
                    mode: mode,
                    includeSHA256: includeSHA256
                )
                guard revision == currentRevision, !Task.isCancelled else { return }
                result = payload.result
                rawJSON = payload.json
            } catch {
                guard revision == currentRevision, !Task.isCancelled else { return }
                failureMessage = error.localizedDescription
            }
            if revision == currentRevision {
                isInspecting = false
            }
        }
    }
}
