import SwiftUI
import MiniPulseCore

struct StatsRows: View {
    @ObservedObject var store: Store

    var body: some View {
        if let s = store.sample {
            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 4) {
                row("Power", s.powerAvailable ? String(format: "%.1f W (SoC %.1f W)", s.sysW, s.socW) : "— (unavailable)")
                row("CPU", String(format: "%.0f%%", s.cpuPct))
                row("Memory", String(format: "%.0f%%", s.memPct))
                if s.powerAvailable {
                    row("Temps", String(format: "CPU %.0f°C · GPU %.0f°C", s.cpuTempC, s.gpuTempC))
                    if s.fanRPM > 0 { row("Fan", "\(s.fanRPM) rpm") }
                }
                row("Energy", String(format: "%.1f Wh total", s.whTotal))
            }
            .font(.system(size: 12))
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

/// Notch/pill content. Collapsed: watts on the left wing, CPU% on
/// the right — the camera housing in the middle is opaque, so
/// nothing useful is drawn under it. Hover expands a flyout below.
struct NotchView: View {
    @ObservedObject var store: Store
    var geometry: NotchGeometry

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text(store.sample?.wattsLabel ?? "—W")
                    .frame(maxWidth: .infinity, alignment: .trailing)
                if geometry.hasNotch {
                    Spacer().frame(width: geometry.notchWidth)
                }
                Text(store.sample.map { String(format: "%.0f%%", $0.cpuPct) } ?? "—%")
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .font(.system(size: 12, weight: .semibold).monospacedDigit())
            .foregroundStyle(.white)
            .frame(height: geometry.collapsedSize.height)
            .padding(.horizontal, 10)

            if store.notchExpanded {
                VStack(alignment: .leading, spacing: 8) {
                    StatsRows(store: store)
                    StateBadge(state: store.state).font(.system(size: 11))
                }
                .foregroundStyle(.white)
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .transition(.opacity)
            }
        }
        .background(Color.black)
        .clipShape(RoundedRectangle(cornerRadius: store.notchExpanded ? 18 : 16))
        .onHover { hovering in
            if hovering {
                withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                    store.notchExpanded = true
                }
            } else {
                // Small debounce so a pass-through to the menu bar
                // does not flicker the flyout.
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) {
                    withAnimation(.easeOut(duration: 0.2)) { store.notchExpanded = false }
                }
            }
        }
    }
}

struct PrefsView: View {
    @ObservedObject var store: Store
    @State private var token: String = TokenStore.load()

    var body: some View {
        Form {
            TextField("Daemon URL", text: $store.host)
                .textFieldStyle(.roundedBorder)
            SecureField("API token (Keychain)", text: $token)
                .textFieldStyle(.roundedBorder)
            Toggle("Show notch/pill panel", isOn: $store.showNotchPanel)
            Text("Local default is http://127.0.0.1:8848. For a remote Mac use its Tailscale Serve (*.ts.net) URL — the daemon refuses plain-LAN binds without a token by design.")
                .font(.system(size: 11)).foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Save & reconnect") {
                    TokenStore.save(token)
                    store.restart()
                }
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(16)
        .frame(width: 420)
    }
}
