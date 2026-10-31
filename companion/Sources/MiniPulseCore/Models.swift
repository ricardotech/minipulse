import Foundation

/// One glanceable reading, decoded from `GET /api/mini` (flat) or
/// adapted from the SSE `GET /api/stream` sample. Field names match
/// the daemon's model.Sample/model.Power vocabulary.
public struct MiniSample: Equatable, Sendable {
    public var sysW: Double
    public var socW: Double
    public var cpuPct: Double
    public var memPct: Double
    public var cpuTempC: Double
    public var gpuTempC: Double
    public var fanRPM: Int
    public var powerAvailable: Bool
    public var whTotal: Double
    /// Root-volume storage; nil when the daemon has no volume data
    /// yet (or predates the disk fields in /api/mini).
    public var diskPct: Double?
    public var diskFreeBytes: UInt64?
    public var diskTotalBytes: UInt64?
    public var topName: String?
    public var topCPUPct: Double?
    /// When the daemon COLLECTED this sample. Freshness must be
    /// judged by this, not by arrival: a daemon whose collector is
    /// wedged but whose HTTP server lives would otherwise serve
    /// hours-old numbers as "live" forever.
    public var time: Date?

    public init(sysW: Double, socW: Double, cpuPct: Double, memPct: Double,
                cpuTempC: Double, gpuTempC: Double, fanRPM: Int,
                powerAvailable: Bool, whTotal: Double,
                topName: String? = nil, topCPUPct: Double? = nil, time: Date? = nil,
                diskPct: Double? = nil, diskFreeBytes: UInt64? = nil, diskTotalBytes: UInt64? = nil) {
        self.sysW = sysW; self.socW = socW; self.cpuPct = cpuPct; self.memPct = memPct
        self.cpuTempC = cpuTempC; self.gpuTempC = gpuTempC; self.fanRPM = fanRPM
        self.powerAvailable = powerAvailable; self.whTotal = whTotal
        self.topName = topName; self.topCPUPct = topCPUPct; self.time = time
        self.diskPct = diskPct; self.diskFreeBytes = diskFreeBytes; self.diskTotalBytes = diskTotalBytes
    }

    /// Watts label for glanceable surfaces. When the daemon reports
    /// power as unavailable this is an em dash — never a fake "0W".
    public var wattsLabel: String {
        guard powerAvailable else { return "—W" }
        return sysW >= 10 ? String(format: "%.0fW", sysW) : String(format: "%.1fW", sysW)
    }

    /// "79.9 GB free of 228 GB" for the popover; nil without disk data.
    public var diskLabel: String? {
        guard let free = diskFreeBytes else { return nil }
        let gb = 1_000_000_000.0
        let f = String(format: "%.1f GB free", Double(free) / gb)
        guard let total = diskTotalBytes, total > 0 else { return f }
        return f + String(format: " of %.0f GB", Double(total) / gb)
    }
}

/// Decoders for the two daemon payloads, kept dependency-free so the
/// core can be unit-tested without AppKit.
public enum MiniPulseJSON {
    /// Go encodes time.Time as RFC3339Nano (up to 9 fractional
    /// digits). ISO8601DateFormatter is picky about fraction width,
    /// so trim the fraction to milliseconds before parsing.
    public static func parseDaemonTime(_ s: String?) -> Date? {
        guard var s else { return nil }
        if let dot = s.firstIndex(of: ".") {
            let tail = s[s.index(after: dot)...]
            let digits = tail.prefix { $0.isNumber }
            if digits.count > 3 {
                let rest = tail.dropFirst(digits.count)
                s = String(s[...dot]) + digits.prefix(3) + rest
            }
        }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }

    private struct MiniPayload: Decodable {
        var sys_w: Double; var soc_w: Double; var cpu_pct: Double; var mem_pct: Double
        var cpu_temp_c: Double; var gpu_temp_c: Double; var fan_rpm: Int
        var power_available: Bool; var wh_total: Double
        var disk_pct: Double?; var disk_free_bytes: UInt64?; var disk_total_bytes: UInt64?
        var time: String?
        var top: Top?
        struct Top: Decodable { var name: String; var cpu_pct: Double }
    }

    private struct StreamPayload: Decodable {
        var cpu_pct: Double; var mem_pct: Double; var wh_total: Double
        var time: String?
        var volumes: [Volume]?
        var power: Power
        struct Volume: Decodable { var used_pct: Double; var free: UInt64; var total: UInt64 }
        struct Power: Decodable {
            var available: Bool; var sys_w: Double; var soc_w: Double
            var cpu_temp_c: Double; var gpu_temp_c: Double; var fan_rpm: Int
        }
    }

    public static func decodeMini(_ data: Data) throws -> MiniSample {
        let p = try JSONDecoder().decode(MiniPayload.self, from: data)
        return MiniSample(sysW: p.sys_w, socW: p.soc_w, cpuPct: p.cpu_pct, memPct: p.mem_pct,
                          cpuTempC: p.cpu_temp_c, gpuTempC: p.gpu_temp_c, fanRPM: p.fan_rpm,
                          powerAvailable: p.power_available, whTotal: p.wh_total,
                          topName: p.top?.name, topCPUPct: p.top?.cpu_pct,
                          time: parseDaemonTime(p.time),
                          diskPct: p.disk_pct, diskFreeBytes: p.disk_free_bytes,
                          diskTotalBytes: p.disk_total_bytes)
    }

    public static func decodeStreamSample(_ data: Data) throws -> MiniSample {
        let p = try JSONDecoder().decode(StreamPayload.self, from: data)
        return MiniSample(sysW: p.power.sys_w, socW: p.power.soc_w, cpuPct: p.cpu_pct,
                          memPct: p.mem_pct, cpuTempC: p.power.cpu_temp_c,
                          gpuTempC: p.power.gpu_temp_c, fanRPM: p.power.fan_rpm,
                          powerAvailable: p.power.available, whTotal: p.wh_total,
                          time: parseDaemonTime(p.time),
                          diskPct: p.volumes?.first?.used_pct,
                          diskFreeBytes: p.volumes?.first?.free,
                          diskTotalBytes: p.volumes?.first?.total)
    }
}
