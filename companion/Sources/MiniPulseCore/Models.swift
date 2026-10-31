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
    public var topName: String?
    public var topCPUPct: Double?

    public init(sysW: Double, socW: Double, cpuPct: Double, memPct: Double,
                cpuTempC: Double, gpuTempC: Double, fanRPM: Int,
                powerAvailable: Bool, whTotal: Double,
                topName: String? = nil, topCPUPct: Double? = nil) {
        self.sysW = sysW; self.socW = socW; self.cpuPct = cpuPct; self.memPct = memPct
        self.cpuTempC = cpuTempC; self.gpuTempC = gpuTempC; self.fanRPM = fanRPM
        self.powerAvailable = powerAvailable; self.whTotal = whTotal
        self.topName = topName; self.topCPUPct = topCPUPct
    }

    /// Watts label for glanceable surfaces. When the daemon reports
    /// power as unavailable this is an em dash — never a fake "0W".
    public var wattsLabel: String {
        guard powerAvailable else { return "—W" }
        return sysW >= 10 ? String(format: "%.0fW", sysW) : String(format: "%.1fW", sysW)
    }
}

/// Decoders for the two daemon payloads, kept dependency-free so the
/// core can be unit-tested without AppKit.
public enum MiniPulseJSON {
    private struct MiniPayload: Decodable {
        var sys_w: Double; var soc_w: Double; var cpu_pct: Double; var mem_pct: Double
        var cpu_temp_c: Double; var gpu_temp_c: Double; var fan_rpm: Int
        var power_available: Bool; var wh_total: Double
        var top: Top?
        struct Top: Decodable { var name: String; var cpu_pct: Double }
    }

    private struct StreamPayload: Decodable {
        var cpu_pct: Double; var mem_pct: Double; var wh_total: Double
        var power: Power
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
                          topName: p.top?.name, topCPUPct: p.top?.cpu_pct)
    }

    public static func decodeStreamSample(_ data: Data) throws -> MiniSample {
        let p = try JSONDecoder().decode(StreamPayload.self, from: data)
        return MiniSample(sysW: p.power.sys_w, socW: p.power.soc_w, cpuPct: p.cpu_pct,
                          memPct: p.mem_pct, cpuTempC: p.power.cpu_temp_c,
                          gpuTempC: p.power.gpu_temp_c, fanRPM: p.power.fan_rpm,
                          powerAvailable: p.power.available, whTotal: p.wh_total)
    }
}
