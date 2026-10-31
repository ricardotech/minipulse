import Foundation

/// Minimal SSE line parser for the daemon's stream: it emits
/// `data: <json>\n\n` per sample plus `: heartbeat` comments.
/// Returns the decoded sample for data lines, nil otherwise
/// (heartbeats, blanks, malformed lines never crash the client).
public enum SSEParser {
    public static func sample(fromLine line: String) -> MiniSample? {
        guard line.hasPrefix("data:") else { return nil }
        let json = line.dropFirst(5).trimmingCharacters(in: .whitespaces)
        guard let data = json.data(using: .utf8) else { return nil }
        return try? MiniPulseJSON.decodeStreamSample(data)
    }
}

/// Maps system load to the menu-bar runner's frame cadence: a calm
/// Mac strolls, a busy one sprints. Original mapping (the concept is
/// RunCat-style; no third-party curves, frames or assets are used).
public enum RunnerCadence {
    public static let minFPS = 2.0
    // Capped at 10: each frame swap makes AppKit refresh the status
    // item's replicants, which measured ~5% of a core at 14 fps
    // under full load. At idle load the cadence (and cost) is ~2 fps.
    public static let maxFPS = 10.0

    public static func fps(cpuPct: Double) -> Double {
        let load = min(max(cpuPct, 0), 100) / 100.0
        return minFPS + load * (maxFPS - minFPS)
    }

    public static func frameInterval(cpuPct: Double) -> TimeInterval {
        1.0 / fps(cpuPct: cpuPct)
    }
}

/// Notch geometry derived ONLY from public NSScreen values, passed
/// in as plain numbers so this stays testable without a display.
/// Width = screen width minus the two auxiliary top areas; height =
/// the top safe-area inset. No per-model dimensions are hardcoded.
public struct NotchGeometry: Equatable, Sendable {
    public var hasNotch: Bool
    public var notchWidth: Double
    public var notchHeight: Double

    public init(screenWidth: Double, safeAreaTop: Double,
                auxLeftWidth: Double?, auxRightWidth: Double?) {
        if safeAreaTop > 0, let l = auxLeftWidth, let r = auxRightWidth {
            hasNotch = true
            notchWidth = max(screenWidth - l - r, 0)
            notchHeight = safeAreaTop
        } else {
            hasNotch = false
            notchWidth = 0
            notchHeight = 0
        }
    }

    /// `-MiniPulseSimulateNotch 185x32` lets a notch-less Mac (the
    /// mini) exercise layout/hover. It validates presentation only —
    /// real safe-area/fullscreen behaviour still needs a notched Mac.
    public static func simulated(from spec: String?) -> NotchGeometry? {
        guard let spec, !spec.isEmpty, spec != "none" else { return nil }
        let parts = spec.lowercased().split(separator: "x")
        guard parts.count == 2, let w = Double(parts[0]), let h = Double(parts[1]),
              w > 0, h > 0 else { return nil }
        var g = NotchGeometry(screenWidth: 0, safeAreaTop: 0, auxLeftWidth: nil, auxRightWidth: nil)
        g.hasNotch = true; g.notchWidth = w; g.notchHeight = h
        return g
    }

    /// Collapsed pill size: with a notch, the pill hugs it (wings on
    /// both sides carry the numbers; the housing itself is opaque).
    /// Without one, a small floating pill where the notch would be.
    public var collapsedSize: (width: Double, height: Double) {
        if hasNotch { return (notchWidth + 150, max(notchHeight, 32)) }
        return (190, 32)
    }
}
