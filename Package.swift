// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "farol-remote",
    platforms: [.macOS(.v14), .iOS(.v17)],
    products: [.library(name: "FarolRemote", targets: ["FarolRemote"])],
    targets: [
        // What a paired phone and the Mac say to each other, and how it is encrypted. It imports nothing but Apple's own.
        .target(name: "FarolRemote"),
        .testTarget(name: "FarolRemoteTests", dependencies: ["FarolRemote"]),
        // A phone in a terminal, to try Remote end to end without the iOS app.
        .executableTarget(name: "RemoteProbe", dependencies: ["FarolRemote"]),
    ],
    swiftLanguageModes: [.v5]
)
