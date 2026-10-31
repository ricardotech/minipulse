import SwiftUI
import MiniPulseCore

struct StatsRows: View {
    @ObservedObject var store: Store

    var body: some View {
        if let s = store.sample {
            VStack(alignment: .leading, spacing: 7) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "bolt.fill").font(.system(size: 12, weight: .semibold))
                    Text(s.wattsLabel).font(.system(size: 22, weight: .semibold).monospacedDigit())
                        .numericTransition()
                    if s.powerAvailable {
                        Text(String(format: "SoC %.1fW", s.socW))
                            .font(.system(size: 11)).foregroundStyle(.secondary)
                    }
                }
                MetricBar(label: "CPU", pct: s.cpuPct)
                MetricBar(label: "MEM", pct: s.memPct)
                Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 4) {
                    if s.powerAvailable {
                        row("Temps", String(format: "CPU %.0f°C · GPU %.0f°C", s.cpuTempC, s.gpuTempC))
                        if s.fanRPM > 0 { row("Fan", "\(s.fanRPM) rpm") }
                    } else {
                        row("Power", "— (unavailable)")
                    }
                    row("Energy", String(format: "%.1f Wh total", s.whTotal))
                }
                .font(.system(size: 12))
            }
        } else {
            Text("Waiting for the daemon…").font(.system(size: 12)).foregroundStyle(.secondary)
        }
    }

    private func row(_ label: String, _ value: String) -> some View {
        GridRow {
            Text(label).foregroundStyle(.secondary)
            Text(value).monospacedDigit()
        }
    }
}

struct StateBadge: View {
    var state: ConnectionState
    var body: some View {
        switch state {
        case .live:
            Label("live", systemImage: "circle.fill").foregroundStyle(.green)
        case .connecting:
            Label("connecting…", systemImage: "circle.dashed").foregroundStyle(.secondary)
        case .stale:
            Label("stale — showing last sample", systemImage: "exclamationmark.circle").foregroundStyle(.orange)
        case .offline(let why):
            Label("offline: \(why)", systemImage: "xmark.circle").foregroundStyle(.red)
        }
    }
}

/// Click panel of the menu-bar item: full stats, top processes,
/// dashboard shortcut. Mirrors the RunCat-style interaction
/// (glance outside, detail on click) with original UI.
struct PopoverView: View {
    @ObservedObject var store: Store
    var openPrefs: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("MiniPulse").font(.headline)
                Spacer()
                StateBadge(state: store.state).font(.system(size: 11))
            }
            StatsRows(store: store)
            if !store.topProcesses.isEmpty {
                Divider()
                Text("Top CPU").font(.system(size: 11)).foregroundStyle(.secondary)
                ForEach(store.topProcesses) { p in
                    HStack {
                        Text(p.name).lineLimit(1)
                        Spacer()
                        Text(String(format: "%.0f%%", p.cpu_pct)).monospacedDigit()
                    }
                    .font(.system(size: 12))
                }
            }
            Divider()
            HStack {
                if let url = store.dashboardURL {
                    Link("Open dashboard", destination: url)
                }
                Spacer()
                Button("Preferences…", action: openPrefs)
                Button("Quit") { NSApp.terminate(nil) }
            }
            .font(.system(size: 12))
        }
        .padding(14)
        .frame(width: 300)
        .onAppear { store.refreshTopProcesses() }
    }
}

extension View {
    @ViewBuilder func numericTransition() -> some View {
        if #available(macOS 14, *) {
            self.contentTransition(.numericText())
        } else {
            self
        }
    }
}

struct MetricBar: View {
    var label: String
    var pct: Double
    var onDark: Bool = false
    var body: some View {
        HStack(spacing: 8) {
            Text(label).font(.system(size: 10, weight: .medium))
                .foregroundStyle(onDark ? AnyShapeStyle(.white.opacity(0.55)) : AnyShapeStyle(.secondary))
                .frame(width: 30, alignment: .leading)
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(onDark ? Color.white.opacity(0.15) : Color.primary.opacity(0.12))
                    Capsule().fill(pct > 90 ? Color.orange : (onDark ? Color.white : Color.primary))
                        .frame(width: max(4, g.size.width * min(max(pct, 0), 100) / 100))
                }
            }
            .frame(height: 4)
            Text(String(format: "%.0f%%", pct))
                .font(.system(size: 11).monospacedDigit()).numericTransition()
                .frame(width: 36, alignment: .trailing)
        }
    }
}

private struct PanelFrameKey: PreferenceKey {
    static var defaultValue: CGRect? = nil
    static func reduce(value: inout CGRect?, nextValue: () -> CGRect?) {
        value = nextValue() ?? value
    }
}

/// Notch/pill panel, Dynamic-Island style: a black silhouette with
/// concave flare into the menu bar grows out of the physical notch
/// on spring physics. At rest it already shows the first two
/// enabled indicators on the wings beside the camera housing;
/// hover expands the full stats card. The window is FIXED size —
/// only this shape animates inside it, and its reported frame lets
/// the controller pass clicks through everywhere else.
struct NotchView: View {
    @ObservedObject var store: Store
    var geometry: NotchGeometry
    var onFrame: (CGRect) -> Void = { _ in }
    @State private var hovering = false

    private var expanded: Bool { store.notchExpanded }
    private var topRadius: CGFloat {
        expanded ? NotchGeometry.openTopRadius
            : (geometry.hasNotch ? NotchGeometry.closedTopRadius : geometry.closedBodySize.height / 2)
    }
    private var bottomRadius: CGFloat {
        expanded ? NotchGeometry.openBottomRadius
            : (geometry.hasNotch ? NotchGeometry.closedBottomRadius : geometry.closedBodySize.height / 2)
    }
    private var shapeSize: CGSize {
        let body = expanded ? NotchGeometry.openSize : geometry.closedBodySize
        let bump = expanded && hovering ? NotchGeometry.hoverBump : 0
        return CGSize(width: body.width + (geometry.hasNotch ? topRadius * 2 : 0) + bump,
                      height: body.height + bump)
    }
    private var openSpring: Animation {
        .spring(response: NotchGeometry.openSpringResponse, dampingFraction: NotchGeometry.openSpringDamping)
    }
    private var closeSpring: Animation {
        .spring(response: NotchGeometry.closeSpringResponse, dampingFraction: NotchGeometry.closeSpringDamping)
    }

    @ViewBuilder private var closedContent: some View {
        if geometry.hasNotch {
            let wings = MenuBarText.wings(sample: store.sample, enabled: store.enabledMetrics)
            HStack(spacing: 0) {
                Text(wings.left).numericTransition()
                    .frame(width: NotchGeometry.notchFlankWidth, alignment: .trailing)
                Spacer().frame(width: geometry.notchWidth)
                Text(wings.right).numericTransition()
                    .frame(width: NotchGeometry.notchFlankWidth, alignment: .leading)
            }
            .font(.system(size: 12, weight: .semibold).monospacedDigit())
            .padding(.horizontal, topRadius)
        } else {
            Text(MenuBarText.build(sample: store.sample, enabled: store.enabledMetrics))
                .numericTransition()
                .font(.system(size: 11.5, weight: .semibold).monospacedDigit())
                .lineLimit(1).minimumScaleFactor(0.75)
                .padding(.horizontal, 12)
        }
    }

    @ViewBuilder private var expandedContent: some View {
        if let s = store.sample {
            VStack(alignment: .leading, spacing: 9) {
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Image(systemName: "bolt.fill").font(.system(size: 13, weight: .semibold))
                    Text(s.wattsLabel).font(.system(size: 27, weight: .semibold).monospacedDigit())
                        .numericTransition()
                    Text(s.powerAvailable ? String(format: "SoC %.1fW", s.socW) : "power unavailable")
                        .font(.system(size: 11)).foregroundStyle(.white.opacity(0.55))
                    Spacer()
                    Circle().fill(store.state == .live ? Color.green : Color.orange)
                        .frame(width: 7, height: 7)
                }
                MetricBar(label: "CPU", pct: s.cpuPct, onDark: true)
                MetricBar(label: "MEM", pct: s.memPct, onDark: true)
                HStack(spacing: 12) {
                    if s.powerAvailable {
                        Label(String(format: "%.0f°C", s.cpuTempC), systemImage: "thermometer.medium")
                        if s.fanRPM > 0 { Label("\(s.fanRPM) rpm", systemImage: "fanblades") }
                    }
                    Spacer()
                    Text(String(format: "%.0f Wh", s.whTotal))
                }
                .font(.system(size: 11).monospacedDigit()).foregroundStyle(.white.opacity(0.75))
                if let top = store.topProcesses.first {
                    Divider().overlay(.white.opacity(0.12))
                    HStack {
                        Text(top.name).lineLimit(1)
                        Spacer()
                        Text(String(format: "%.0f%% CPU", top.cpu_pct)).monospacedDigit()
                    }
                    .font(.system(size: 11)).foregroundStyle(.white.opacity(0.75))
                }
            }
        } else {
            Text(store.state == .connecting ? "Connecting…" : "Waiting for the daemon…")
                .font(.system(size: 12)).foregroundStyle(.white.opacity(0.7))
        }
    }

    var body: some View {
        // Top-aligned everywhere: a centered ZStack taller than the
        // collapsed shape would push the closed band half above the
        // clip (observed: black silhouette, wing text vanished).
        ZStack(alignment: .top) {
            expandedContent
                .padding(.top, geometry.hasNotch ? 28 : 16)
                .padding(.horizontal, 22)
                .padding(.bottom, 14)
                .frame(width: shapeSize.width, height: NotchGeometry.openSize.height,
                       alignment: .topLeading)
                // Dissolve instead of being clipped by the collapsing shape.
                .blur(radius: expanded ? 0 : 40)
                .opacity(expanded ? 1 : 0)
                .scaleEffect(expanded ? 1 : 0.3, anchor: .top)
            closedContent
                .frame(width: shapeSize.width, height: geometry.closedBodySize.height)
                .opacity(expanded ? 0 : 1)
        }
        .foregroundStyle(.white)
        .frame(width: shapeSize.width, height: shapeSize.height, alignment: .top)
        .background(Color.black)
        .clipShape(NotchShape(topRadius: topRadius, bottomRadius: bottomRadius, hasNotch: geometry.hasNotch))
        .background(GeometryReader { proxy in
            Color.clear.preference(key: PanelFrameKey.self,
                                   value: proxy.frame(in: .named("MiniPulseNotchRoot")))
        })
        .shadow(color: .black.opacity(expanded ? (hovering ? 0.55 : 0.3) : 0), radius: 9)
        .offset(y: geometry.hasNotch ? 0 : NotchGeometry.pillTopGap)
        .animation(.easeOut(duration: 0.18), value: hovering)
        .onTapGesture {
            if let url = store.dashboardURL { NSWorkspace.shared.open(url) }
        }
        .onHover { inside in
            hovering = inside
            if inside {
                NSHapticFeedbackManager.defaultPerformer.perform(.generic, performanceTime: .default)
                store.refreshTopProcesses()
                withAnimation(openSpring) { store.notchExpanded = true }
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) {
                    if !hovering { withAnimation(closeSpring) { store.notchExpanded = false } }
                }
            }
        }
        .frame(width: geometry.windowSize.width, height: geometry.windowSize.height, alignment: .top)
        .coordinateSpace(name: "MiniPulseNotchRoot")
        .onPreferenceChange(PanelFrameKey.self) { rect in
            if let rect { onFrame(rect) }
        }
    }
}

struct PrefsView: View {
    @ObservedObject var store: Store
    // Local draft: binding the field straight to store.host would
    // persist every keystroke (didSet → UserDefaults) BEFORE
    // validation — including a pasted credential-bearing URL.
    @State private var hostText: String
    @State private var token: String = TokenStore.load()
    @State private var problem: String?

    init(store: Store) {
        self.store = store
        _hostText = State(initialValue: store.host)
    }

    private var hostWarning: String? {
        switch HostValidation.validate(hostText) {
        case .invalid(let why): return "Invalid URL: \(why)."
        case .ok(warnInsecure: true):
            return "Plain http outside loopback: the token travels in clear on the LAN. Prefer a Tailscale Serve (*.ts.net) URL."
        case .ok: return nil
        }
    }

    var body: some View {
        Form {
            TextField("Daemon URL", text: $hostText)
                .textFieldStyle(.roundedBorder)
            if let hostWarning {
                Text(hostWarning).font(.system(size: 11)).foregroundStyle(.orange)
            }
            SecureField("API token (Keychain)", text: $token)
                .textFieldStyle(.roundedBorder)
            if let problem {
                Text(problem).font(.system(size: 11)).foregroundStyle(.red)
            }
            Toggle("Show notch/pill panel", isOn: $store.showNotchPanel)
            Toggle("Animated pulse icon in the menu bar", isOn: $store.showRunner)
            VStack(alignment: .leading, spacing: 5) {
                Text("Menu bar indicators").font(.system(size: 12, weight: .medium))
                HStack(spacing: 12) {
                    ForEach(MenuBarMetric.allCases, id: \.rawValue) { metric in
                        Toggle(metric.label, isOn: Binding(
                            get: { store.enabledMetrics.contains(metric) },
                            set: { _ in store.toggleMetric(metric) }))
                            .toggleStyle(.checkbox)
                            .font(.system(size: 12))
                    }
                }
                Text("Shown side by side, e.g. 18W · CPU 23% · MEM 61%. The first two also ride the notch wings.")
                    .font(.system(size: 11)).foregroundStyle(.secondary)
            }
            Text("Local default is http://127.0.0.1:8848. For a remote Mac use its Tailscale Serve (*.ts.net) URL — the daemon refuses plain-LAN binds without a token by design.")
                .font(.system(size: 11)).foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Save & reconnect") {
                    if case .invalid(let why) = HostValidation.validate(hostText) {
                        problem = "Fix the URL first: \(why)."
                        return
                    }
                    if TokenStore.save(token) {
                        store.host = hostText.trimmingCharacters(in: .whitespacesAndNewlines)
                        problem = nil
                        store.restart()
                    } else {
                        problem = "Keychain refused to store the token (ad-hoc rebuilds change the app identity — delete the MiniPulseNotch Keychain item and try again)."
                    }
                }
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(16)
        .frame(width: 420)
    }
}
