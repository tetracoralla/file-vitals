import Foundation

enum LaunchArguments {
    static var inspectionURL: URL? {
        let arguments = CommandLine.arguments
        guard let flag = arguments.firstIndex(of: "--inspect"), arguments.indices.contains(flag + 1) else {
            return nil
        }
        return URL(fileURLWithPath: arguments[flag + 1])
    }
}
