// swift-tools-version: 5.9
import PackageDescription

// MiniPulseMenu — the optional menu-bar companion for the headless
// MiniPulse daemon (issue #2). Clean-room Swift, public AppKit APIs
// only. The daemon stays deliberately NOT a menu-bar app; this is a
// separate client of its HTTP API.
let package = Package(
    name: "MiniPulseMenu",
    platforms: [.macOS(.v13)],
    targets: [
        .target(name: "MiniPulseCore"),
        .executableTarget(name: "MiniPulseMenu", dependencies: ["MiniPulseCore"]),
        .testTarget(name: "MiniPulseCoreTests", dependencies: ["MiniPulseCore"]),
    ]
)
