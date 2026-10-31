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
                if let disk = s.diskPct {
                    MetricBar(label: "DISK", pct: disk)
                }
                Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 4) {
                    if s.powerAvailable {
                        row("Temps", String(format: "CPU %.0f°C · GPU %.0f°C", s.cpuTempC, s.gpuTempC))
                        if s.fanRPM > 0 { row("Fan", "\(s.fanRPM) rpm") }
                    } else {
                        row("Power", "— (unavailable)")
                    }
                    row("Energy", String(format: "%.1f Wh total", s.whTotal))
                    if let storage = s.diskLabel { row("Storage", storage) }
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
    var body: some View {
        HStack(spacing: 8) {
            Text(label).font(.system(size: 10, weight: .medium))
                .foregroundStyle(.secondary)
                .frame(width: 30, alignment: .leading)
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.primary.opacity(0.12))
                    Capsule().fill(pct > 90 ? Color.orange : Color.primary)
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
            Toggle("Animated pulse icon in the menu bar", isOn: $store.showRunner)
            VStack(alignment: .leading, spacing: 5) {
                Text("Menu bar indicators").font(.system(size: 12, weight: .medium))
                // One toggle per row: side-by-side checkboxes squeeze
                // the labels unreadable in a fixed-width form.
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(MenuBarMetric.allCases, id: \.rawValue) { metric in
                        Toggle(metric.label, isOn: Binding(
                            get: { store.enabledMetrics.contains(metric) },
                            set: { _ in store.toggleMetric(metric) }))
                            .toggleStyle(.checkbox)
                            .font(.system(size: 12))
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                Text("Shown side by side, e.g. 18W · CPU 23% · MEM 61%.")
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
                        problem = "Keychain refused to store the token (ad-hoc rebuilds change the app identity — delete the MiniPulseMenu Keychain item and try again)."
                    }
                }
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(16)
        .frame(width: 420)
    }
}
