import SwiftUI

struct InspectionFailureView: View {
    let message: String
    let retry: () -> Void
    let chooseFile: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label("Couldn’t inspect this file", systemImage: "exclamationmark.triangle")
        } description: {
            Text(message)
        } actions: {
            HStack {
                Button("Try Again", action: retry)
                    .buttonStyle(.borderedProminent)
                Button("Choose Another File", action: chooseFile)
            }
        }
    }
}
