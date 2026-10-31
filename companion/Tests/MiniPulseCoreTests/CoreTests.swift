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
        XCTAssertEqual(g.collapsedSize.width, NotchGeometry.pillClosedSize.width)
        // Window envelope always fits the open size plus shadow room.
        XCTAssertGreaterThan(g.windowSize.width, NotchGeometry.openSize.width)
        XCTAssertGreaterThan(g.windowSize.height, NotchGeometry.openSize.height)
    }

    func testGeometryClosedNotchBodyHasWings() {
        let g = NotchGeometry(screenWidth: 1512, safeAreaTop: 32,
                              auxLeftWidth: 663.5, auxRightWidth: 663.5)
        // 185pt cutout + one flank per side for the wing numbers.
        XCTAssertEqual(g.closedBodySize.width, 185 + NotchGeometry.notchFlankWidth * 2, accuracy: 0.001)
        XCTAssertEqual(g.closedBodySize.height, 32)
    }

    func testSimulatedSpecParsing() {
        XCTAssertEqual(NotchGeometry.simulated(from: "185x32")?.notchWidth, 185)
        XCTAssertNil(NotchGeometry.simulated(from: "none"))
        XCTAssertNil(NotchGeometry.simulated(from: "banana"))
        XCTAssertNil(NotchGeometry.simulated(from: nil))
        // Absurd specs clamp instead of building a giant window.
        XCTAssertEqual(NotchGeometry.simulated(from: "99999x99999")?.notchWidth, 400)
    }

    func testDaemonTimeParsesGoNanoAndDrivesFreshness() throws {
        let json = #"{"sys_w":1,"soc_w":1,"cpu_pct":1,"mem_pct":1,"cpu_temp_c":1,"gpu_temp_c":1,"fan_rpm":1,"power_available":true,"wh_total":1,"time":"2026-10-31T10:15:11.619407-03:00"}"#
        let s = try MiniPulseJSON.decodeMini(Data(json.utf8))
        XCTAssertNotNil(s.time)
        XCTAssertEqual(s.time?.timeIntervalSince1970 ?? 0, 1793452511.619, accuracy: 0.01)
        XCTAssertNil(MiniPulseJSON.parseDaemonTime("not-a-date"))
        XCTAssertNil(MiniPulseJSON.parseDaemonTime(nil))
    }

    func testMenuBarTextBuildsEnabledSubsetInCanonicalOrder() {
        let s = MiniSample(sysW: 18.4, socW: 9, cpuPct: 23, memPct: 61, cpuTempC: 64,
                           gpuTempC: 58, fanRPM: 1200, powerAvailable: true, whTotal: 10)
        // Default trio the user asked for: watts + CPU% + MEM%.
        XCTAssertEqual(MenuBarText.build(sample: s, enabled: MenuBarText.defaultMetrics),
                       "18W · CPU 23% · MEM 61%")
        // Toggle order does not matter; canonical order wins.
        XCTAssertEqual(MenuBarText.build(sample: s, enabled: [.memory, .watts]),
                       "18W · MEM 61%")
        XCTAssertEqual(MenuBarText.build(sample: s, enabled: [.fan, .cpuTemp]),
                       "64° · 1200 rpm")
        XCTAssertEqual(MenuBarText.build(sample: nil, enabled: MenuBarText.defaultMetrics), "")
        XCTAssertEqual(MenuBarText.build(sample: s, enabled: []), "")
        // Fanless Mac: fan metric drops out instead of showing 0.
        var fanless = s; fanless.fanRPM = 0
        XCTAssertEqual(MenuBarText.build(sample: fanless, enabled: [.fan]), "")
        // Wings: bare values, watts left / CPU right by default.
        let w = MenuBarText.wings(sample: s, enabled: MenuBarText.defaultMetrics)
        XCTAssertEqual(w.left, "18W")
        XCTAssertEqual(w.right, "23%")
    }

    func testHostValidation() {
        XCTAssertEqual(HostValidation.validate("http://127.0.0.1:8848"), .ok(warnInsecure: false))
        XCTAssertEqual(HostValidation.validate("https://mini.tail-abc.ts.net"), .ok(warnInsecure: false))
        XCTAssertEqual(HostValidation.validate("http://192.168.1.9:8848"), .ok(warnInsecure: true))
        if case .ok = HostValidation.validate("127.0.0.1:8848") { XCTFail("missing scheme must be invalid") }
        if case .ok = HostValidation.validate("http://h:8848/?token=SECRET") { XCTFail("query token must be invalid") }
        if case .ok = HostValidation.validate("http://user:pw@h:8848") { XCTFail("userinfo must be invalid") }
    }
}
