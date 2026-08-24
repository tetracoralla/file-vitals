import Foundation

enum DropSelection {
    static func firstFileURL(in urls: [URL]) -> URL? {
        guard let url = urls.first, url.isFileURL else { return nil }
        return url
    }
}
