import Foundation
import os

enum InspectorClientError: LocalizedError {
    case executableMissing
    case launchFailed(String)
    case invalidResponse(String)

    var errorDescription: String? {
        switch self {
        case .executableMissing:
            "The File Vitals inspection engine is missing from this app."
        case let .launchFailed(message):
            message.isEmpty ? "The inspection engine could not be started." : message
        case let .invalidResponse(message):
            message.isEmpty ? "The inspection engine returned an invalid result." : message
        }
    }
}

/// A process whose launch and termination are serialized behind a lock, so a
/// cancellation that lands before launch is honored instead of racing, and one
/// that lands later terminates the running engine instead of orphaning it.
final class CancellableProcess: @unchecked Sendable {
    private let lock = NSLock()
    private var process: Process?
    private var cancelled = false

    func launch(_ configure: () throws -> Process) throws -> Process {
        lock.lock()
        defer { lock.unlock() }
        if cancelled {
            throw CancellationError()
        }
        let process = try configure()
        try process.run()
        self.process = process
        return process
    }

    func cancel() {
        lock.lock()
        defer { lock.unlock() }
        cancelled = true
        if let process, process.isRunning {
            process.terminate()
        }
    }
}

final class InspectorClient: Sendable {
    private static let logger = Logger(subsystem: "org.openadam.file-vitals", category: "inspection")

    func inspect(url: URL, mode: InspectionMode, includeSHA256: Bool) async throws -> InspectionPayload {
        let executable = try Self.resolveExecutable()
        let handle = CancellableProcess()
        return try await withTaskCancellationHandler {
            try await Self.run(handle: handle, executable: executable, url: url, mode: mode, includeSHA256: includeSHA256)
        } onCancel: {
            handle.cancel()
        }
    }

    private static func resolveExecutable() throws -> URL {
        let fileManager = FileManager.default
        #if DEBUG
        if let override = ProcessInfo.processInfo.environment["FILE_VITALS_CLI"], !override.isEmpty {
            let url = URL(fileURLWithPath: override)
            if fileManager.isExecutableFile(atPath: url.path) {
                return url
            }
        }
        #endif
        if let bundled = Bundle.main.resourceURL?
            .appendingPathComponent("runtime", isDirectory: true)
            .appendingPathComponent("finspect"),
           fileManager.isExecutableFile(atPath: bundled.path)
        {
            return bundled
        }
        #if DEBUG
        let development = URL(fileURLWithPath: fileManager.currentDirectoryPath)
            .appendingPathComponent("bin/finspect")
        if fileManager.isExecutableFile(atPath: development.path) {
            return development
        }
        #endif
        throw InspectorClientError.executableMissing
    }

    private static func run(
        handle: CancellableProcess,
        executable: URL,
        url: URL,
        mode: InspectionMode,
        includeSHA256: Bool
    ) async throws -> InspectionPayload {
        let stdout = Pipe()
        let stderr = Pipe()
        let process = try handle.launch {
            let process = Process()
            process.executableURL = executable
            // The deadline is passed explicitly so the app's dependence on the
            // engine's own budget is auditable rather than implicit.
            process.arguments = [url.path, "--mode", mode.rawValue, "--timeout", "10s", "--json"]
                + (includeSHA256 ? ["--sha256"] : [])
            process.standardOutput = stdout
            process.standardError = stderr
            return process
        }
        logger.log("inspection started (mode: \(mode.rawValue, privacy: .public), sha256: \(includeSHA256, privacy: .public))")
        // Detached readers keep both pipes draining while the process runs, so
        // a large result can never dead-lock on a full pipe buffer.
        let stdoutReader = Task.detached {
            stdout.fileHandleForReading.readDataToEndOfFile()
        }
        let stderrReader = Task.detached {
            stderr.fileHandleForReading.readDataToEndOfFile()
        }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            process.terminationHandler = { _ in continuation.resume() }
        }
        let output = await stdoutReader.value
        let errorOutput = await stderrReader.value
        try Task.checkCancellation()

        guard let json = String(data: output, encoding: .utf8), !json.isEmpty else {
            let message = String(data: errorOutput, encoding: .utf8) ?? ""
            logger.error("inspection produced no output (mode: \(mode.rawValue, privacy: .public))")
            throw InspectorClientError.invalidResponse(message.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        do {
            let decoder = JSONDecoder()
            decoder.keyDecodingStrategy = .convertFromSnakeCase
            let payload = InspectionPayload(result: try decoder.decode(InspectionResult.self, from: output), json: json)
            logger.log("inspection finished (status: \(payload.result.status, privacy: .public))")
            return payload
        } catch {
            logger.error("inspection output failed to decode (mode: \(mode.rawValue, privacy: .public))")
            throw InspectorClientError.invalidResponse(error.localizedDescription)
        }
    }
}
