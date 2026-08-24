import SwiftUI
import UniformTypeIdentifiers

struct ContentView: View {
    @State private var store = InspectionStore()
    @State private var showsFileImporter = false
    @State private var isDropTarget = false
    @State private var appliedLaunchArgument = false

    var body: some View {
        Group {
            if store.selectedURL == nil {
                EmptyInspectionView(
                    chooseFile: { showsFileImporter = true }
                )
            } else if store.isInspecting {
                VStack(spacing: 12) {
                    ProgressView()
                        .controlSize(.large)
                    Text("Inspecting \(store.selectedURL?.lastPathComponent ?? "file")…")
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let message = store.failureMessage {
                InspectionFailureView(
                    message: message,
                    retry: store.inspect,
                    chooseFile: { showsFileImporter = true }
                )
            } else if let result = store.result, let url = store.selectedURL {
                InspectionView(result: result, sourceURL: url, rawJSON: store.rawJSON)
            }
        }
        .toolbar {
            ToolbarItem(placement: .navigation) {
                Button {
                    showsFileImporter = true
                } label: {
                    Label("Open File", systemImage: "folder")
                }
                .keyboardShortcut("o", modifiers: .command)
            }
            if store.selectedURL != nil {
                ToolbarItemGroup(placement: .primaryAction) {
                    Picker("Depth", selection: $store.mode) {
                        ForEach(InspectionMode.allCases) { mode in
                            Text(mode.label).tag(mode)
                        }
                    }
                    .pickerStyle(.segmented)
                    .frame(width: 230)

                    Toggle("SHA-256", systemImage: "number", isOn: $store.includeSHA256)
                        .help("Include a SHA-256 digest")

                    Button {
                        store.inspect()
                    } label: {
                        Label("Inspect", systemImage: "arrow.clockwise")
                    }
                    .keyboardShortcut("r", modifiers: .command)
                    .disabled(store.isInspecting)
                }
            }
        }
        .fileImporter(
            isPresented: $showsFileImporter,
            allowedContentTypes: [.item],
            allowsMultipleSelection: false
        ) { selection in
            if case let .success(urls) = selection, let url = urls.first {
                store.select(url)
            }
        }
        .dropDestination(for: URL.self) { urls, _ in
            // Only local file URLs are inspectable; a web link dragged from a
            // browser would become a garbage engine path.
            guard let url = DropSelection.firstFileURL(in: urls) else { return false }
            store.select(url)
            return true
        } isTargeted: { targeted in
            isDropTarget = targeted
        }
        .overlay {
            RoundedRectangle(cornerRadius: 16)
                .stroke(isDropTarget ? Color.accentColor : Color.clear, lineWidth: 2)
                .padding(24)
                .allowsHitTesting(false)
        }
        .onChange(of: store.mode) {
            store.inspect()
        }
        .onChange(of: store.includeSHA256) {
            store.inspect()
        }
        .task {
            guard !appliedLaunchArgument else { return }
            appliedLaunchArgument = true
            if let url = LaunchArguments.inspectionURL {
                store.select(url)
            }
        }
    }
}
