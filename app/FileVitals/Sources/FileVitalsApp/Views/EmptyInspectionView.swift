import SwiftUI

struct EmptyInspectionView: View {
    let chooseFile: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label("Choose or drop a file", systemImage: "doc.text.magnifyingglass")
        } actions: {
            Button("Choose File", action: chooseFile)
                .buttonStyle(.borderedProminent)
        }
    }
}
