import XCTest
@testable import MiniPulseCore

final class CoreTests: XCTestCase {
    func testMiniDecodeAndWattsLabel() throws {
        let json = #"{"sys_w":31.2,"soc_w":8.4,"cpu_pct":11.5,"mem_pct":56,"cpu_temp_c":63,"gpu_temp_c":58,"fan_rpm":1000,"power_available":true,"wh_total":196.1,"top":{"name":"node","cpu_pct":120}}"#
        let s = try MiniPulseJSON.decodeMini(Data(json.utf8))
        XCTAssertEqual(s.sysW, 31.2, accuracy: 0.001)
        XCTAssertEqual(s.wattsLabel, "31W")
        XCTAssertEqual(s.topName, "node")
    }

    func testUnavailablePowerIsDashNeverZeroWatts() {
        let s = MiniSample(sysW: 0, socW: 0, cpuPct: 5, memPct: 40, cpuTempC: 0,
                           gpuTempC: 0, fanRPM: 0, powerAvailable: false, whTotal: 0)
        XCTAssertEqual(s.wattsLabel, "—W")
    }

    func testSSEParserSkipsHeartbeatsAndGarbage() {
        XCTAssertNil(SSEParser.sample(fromLine: ": heartbeat"))
        XCTAssertNil(SSEParser.sample(fromLine: ""))
        XCTAssertNil(SSEParser.sample(fromLine: "data: not-json"))
        let line = #"data: {"cpu_pct":50,"mem_pct":60,"wh_total":10,"power":{"available":true,"sys_w":42,"soc_w":20,"cpu_temp_c":70,"gpu_temp_c":65,"fan_rpm":1200}}"#
        let s = SSEParser.sample(fromLine: line)
        XCTAssertEqual(s?.sysW, 42)
        XCTAssertEqual(s?.cpuPct, 50)
    }

    func testCadenceMonotonicAndClamped() {
        XCTAssertEqual(RunnerCadence.fps(cpuPct: 0), RunnerCadence.minFPS)
        XCTAssertEqual(RunnerCadence.fps(cpuPct: 100), RunnerCadence.maxFPS)
        XCTAssertEqual(RunnerCadence.fps(cpuPct: 900), RunnerCadence.maxFPS)
        XCTAssertLessThan(RunnerCadence.fps(cpuPct: 20), RunnerCadence.fps(cpuPct: 80))
    }

    func testGeometryDerivesNotchFromAuxAreas() {
        // Measured on a real MacBookPro18,4: 1512pt wide, aux 663.5 each side.
        let g = NotchGeometry(screenWidth: 1512, safeAreaTop: 32,
                              auxLeftWidth: 663.5, auxRightWidth: 663.5)
        XCTAssertTrue(g.hasNotch)
        XCTAssertEqual(g.notchWidth, 185, accuracy: 0.001)
        XCTAssertEqual(g.notchHeight, 32)
    }

    func testGeometryNoNotchFallsBackToPill() {
        let g = NotchGeometry(screenWidth: 3440, safeAreaTop: 0,
                              auxLeftWidth: nil, auxRightWidth: nil)
        XCTAssertFalse(g.hasNotch)
        XCTAssertEqual(g.collapsedSize.width, 190)
    }

    func testSimulatedSpecParsing() {
        XCTAssertEqual(NotchGeometry.simulated(from: "185x32")?.notchWidth, 185)
        XCTAssertNil(NotchGeometry.simulated(from: "none"))
        XCTAssertNil(NotchGeometry.simulated(from: "banana"))
        XCTAssertNil(NotchGeometry.simulated(from: nil))
    }
}
