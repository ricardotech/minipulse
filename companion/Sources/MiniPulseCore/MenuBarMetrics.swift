import Foundation

/// One glanceable indicator the user can pin to the menu bar (and
/// to the closed notch/pill wings). Canonical display order is the
/// declaration order; users toggle any subset, including several
/// at once — e.g. watts + CPU % + memory %.
public enum MenuBarMetric: String, CaseIterable, Codable, Sendable {
    case watts
    case cpu
    case memory
    case cpuTemp
    case fan

    public var label: String {
        switch self {
        case .watts: return "Watts"
        case .cpu: return "CPU"
        case .memory: return "Memory"
        case .cpuTemp: return "CPU temp"
        case .fan: return "Fan"
        }
    }

    /// Short value for the given sample, or nil when the metric is
    /// meaningless right now (fanless Mac, no sample yet). Power
    /// being unavailable degrades watts/temp to an em dash — never
    /// a fake zero.
    public func value(in s: MiniSample?) -> String? {
        guard let s else { return nil }
        switch self {
        case .watts:
            return s.wattsLabel
        case .cpu:
            return String(format: "%.0f%%", s.cpuPct)
        case .memory:
            return String(format: "%.0f%%", s.memPct)
        case .cpuTemp:
            return s.powerAvailable ? String(format: "%.0f°", s.cpuTempC) : "—°"
        case .fan:
            return s.fanRPM > 0 ? "\(s.fanRPM)" : nil
        }
    }

    /// With a unit/label prefix where the bare value is ambiguous
    /// (two bare percents side by side need telling apart).
    public func labeledValue(in s: MiniSample?) -> String? {
        guard let v = value(in: s) else { return nil }
        switch self {
        case .cpu: return "CPU \(v)"
        case .memory: return "MEM \(v)"
        case .fan: return "\(v) rpm"
        case .watts, .cpuTemp: return v
        }
    }
}

public enum MenuBarText {
    public static let defaultMetrics: [MenuBarMetric] = [.watts, .cpu, .memory]

    /// "18W · CPU 23% · MEM 61%" for the enabled subset, in
    /// canonical order regardless of toggle order. Empty when
    /// nothing is enabled or no sample has arrived.
    public static func build(sample: MiniSample?, enabled: [MenuBarMetric]) -> String {
        let ordered = MenuBarMetric.allCases.filter { enabled.contains($0) }
        return ordered.compactMap { $0.labeledValue(in: sample) }.joined(separator: " · ")
    }

    /// Wing pair for the closed notch: first enabled metric on the
    /// left wing, second on the right (bare values — the wings are
    /// too narrow for labels).
    public static func wings(sample: MiniSample?, enabled: [MenuBarMetric]) -> (left: String, right: String) {
        let ordered = MenuBarMetric.allCases.filter { enabled.contains($0) }
        let left = ordered.first.flatMap { $0.value(in: sample) }
        let right = ordered.dropFirst().first.flatMap { $0.value(in: sample) }
        return (left ?? "—W", right ?? (sample.map { String(format: "%.0f%%", $0.cpuPct) } ?? "—%"))
    }
}
