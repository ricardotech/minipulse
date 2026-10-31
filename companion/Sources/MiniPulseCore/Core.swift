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
        guard parts.count == 2, var w = Double(parts[0]), var h = Double(parts[1]),
              w > 0, h > 0 else { return nil }
        // Clamp to plausible notch territory: a typo'd default must
        // not produce a screen-sized window.
        w = min(max(w, 80), 400); h = min(max(h, 20), 60)
        var g = NotchGeometry(screenWidth: 0, safeAreaTop: 0, auxLeftWidth: nil, auxRightWidth: nil)
        g.hasNotch = true; g.notchWidth = w; g.notchHeight = h
        return g
    }

    // Silhouette tuning, in the spirit of the well-crafted open
    // source notch overlays (Glance, MIT): concave flare into the
    // menu bar on top, big continuous bottom corners when open.
    public static let closedTopRadius: Double = 8
    public static let closedBottomRadius: Double = 12
    public static let openTopRadius: Double = 16
    public static let openBottomRadius: Double = 44
    /// Resting notch state already shows stats on wings flanking
    /// the physical cutout, so the closed body is the cutout plus
    /// one flank per side.
    public static let notchFlankWidth: Double = 52
    public static let openSize: (width: Double, height: Double) = (304, 174)
    public static let pillClosedSize: (width: Double, height: Double) = (176, 30)
    public static let pillTopGap: Double = 3
    /// Room for the in-content shadow (the window has none).
    public static let shadowPadding: Double = 24
    public static let hoverBump: Double = 6

    // Open/close springs: opening overshoots slightly, closing is
    // critically damped.
    public static let openSpringResponse: Double = 0.45
    public static let openSpringDamping: Double = 0.7
    public static let closeSpringResponse: Double = 0.45
    public static let closeSpringDamping: Double = 1.0

    /// Visible black body at rest (before flare allowance).
    public var closedBodySize: (width: Double, height: Double) {
        if hasNotch {
            return (notchWidth + Self.notchFlankWidth * 2, max(notchHeight, 32))
        }
        return Self.pillClosedSize
    }

    /// Fixed window envelope: created once, never resized. All
    /// growth is SwiftUI animating the shape inside it.
    public var windowSize: (width: Double, height: Double) {
        let contentWidth = max(Self.openSize.width, closedBodySize.width)
            + (hasNotch ? Self.openTopRadius * 2 : 0)
        return (
            width: contentWidth + Self.shadowPadding * 2 + Self.hoverBump,
            height: Self.openSize.height + Self.shadowPadding + Self.hoverBump
                + (hasNotch ? 0 : Self.pillTopGap)
        )
    }

    /// Back-compat for callers that only need a resting size.
    public var collapsedSize: (width: Double, height: Double) { closedBodySize }
}

/// The daemon base URL is free text in Preferences and persisted in
/// UserDefaults. Validate it before use: a missing scheme used to
/// spin the reconnect loop; userinfo/query would persist a secret
/// outside the Keychain. Plain http is fine on loopback and stays
/// allowed elsewhere only as an explicit, warned-about choice
/// (the token then travels in clear — prefer a *.ts.net URL).
public enum HostValidation {
    public enum Result: Equatable {
        case ok(warnInsecure: Bool)
        case invalid(String)
    }

    public static func validate(_ raw: String) -> Result {
        let s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: s), let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https", url.host != nil else {
            return .invalid("use a full URL, e.g. http://127.0.0.1:8848")
        }
        if url.user != nil || url.password != nil {
            return .invalid("credentials belong in the token field (Keychain), not the URL")
        }
        if url.query != nil || url.fragment != nil {
            return .invalid("no query/fragment in the base URL — the token field is separate")
        }
        let loopback = ["127.0.0.1", "localhost", "::1"].contains(url.host?.lowercased() ?? "")
        return .ok(warnInsecure: scheme == "http" && !loopback)
    }
}
