// swift-tools-version: 6.1

import PackageDescription

let package = Package(
    name: "FileVitals",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "FileVitals", targets: ["FileVitalsApp"]),
    ],
    targets: [
        .executableTarget(name: "FileVitalsApp"),
        .testTarget(name: "FileVitalsAppTests", dependencies: ["FileVitalsApp"]),
    ]
)
