// swift-tools-version: 5.9
import PackageDescription

// MiniPulseNotch — the optional GUI companion for the headless
// MiniPulse daemon (issue #2). Clean-room Swift, public AppKit APIs
// only. The daemon stays deliberately NOT a menu-bar app; this is a
// separate client of its HTTP API.
let package = Package(
    name: "MiniPulseNotch",
    platforms: [.macOS(.v13)],
    targets: [
        .target(name: "MiniPulseCore"),
        .executableTarget(name: "MiniPulseNotch", dependencies: ["MiniPulseCore"]),
        .testTarget(name: "MiniPulseCoreTests", dependencies: ["MiniPulseCore"]),
    ]
)
