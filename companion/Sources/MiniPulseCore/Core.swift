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
