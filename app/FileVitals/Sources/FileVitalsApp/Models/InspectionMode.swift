import Foundation

enum InspectionMode: String, CaseIterable, Identifiable, Sendable {
    case quick
    case standard
    case deep

    var id: Self { self }

    var label: String {
        switch self {
        case .quick: "Quick"
        case .standard: "Standard"
        case .deep: "Deep"
        }
    }
}
