import AppKit
import SwiftUI
import Combine
import MiniPulseCore

/// Original menu-bar runner: a pulse waveform whose scroll speed
/// tracks CPU load (calm Mac, slow pulse; busy Mac, fast pulse).
/// Frames are drawn in code — no third-party sprites or assets.
enum PulseFrames {
    static let count = 8
    // Pre-rendered once: redrawing an NSImage per frame at up to
    // 14 fps was several percent of a core for a 34pt squiggle.
    private static let cache: [NSImage] = (0..<count).map { draw($0) }
    static func frame(_ index: Int) -> NSImage { cache[index % count] }
    private static func draw(_ index: Int) -> NSImage {
        let size = NSSize(width: 34, height: 18)
        let img = NSImage(size: size)
        img.lockFocus()
        NSColor.black.setStroke()
        let path = NSBezierPath()
        path.lineWidth = 1.6
        path.lineJoinStyle = .round
        let mid = size.height / 2
        let phase = Double(index % count) / Double(count) * 12.0
        // One heartbeat (flat, spike, dip, flat) every 12pt, scrolled.
        path.move(to: NSPoint(x: 0, y: mid))
        var x = -phase
        while x < Double(size.width) {
            path.line(to: NSPoint(x: x + 4, y: mid))
            path.line(to: NSPoint(x: x + 6, y: mid + 6))
            path.line(to: NSPoint(x: x + 8, y: mid - 4))
            path.line(to: NSPoint(x: x + 12, y: mid))
            x += 12
        }
        path.stroke()
        img.unlockFocus()
        img.isTemplate = true
        return img
    }
}

@MainActor
final class StatusItemController: NSObject {
    private let store: Store
    private let item: NSStatusItem
    private let popover = NSPopover()
    private var timer: Timer?
    private var frameIndex = 0
    private var cancellables: Set<AnyCancellable> = []
    private var paused = false
    var openPrefs: () -> Void = {}

    init(store: Store) {
        self.store = store
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        super.init()
        item.button?.image = PulseFrames.frame(0)
        item.button?.imagePosition = .imageLeading
        item.button?.font = .monospacedDigitSystemFont(ofSize: 12, weight: .medium)
        item.button?.target = self
        item.button?.action = #selector(togglePopover)
        popover.behavior = .transient
        popover.contentViewController = NSHostingController(
            rootView: PopoverView(store: store, openPrefs: { [weak self] in
                self?.popover.performClose(nil)
                self?.openPrefs()
            }))
        store.$sample
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in
                self?.refreshTitle()
                self?.reschedule(cpu: self?.store.sample?.cpuPct ?? 0)
            }
            .store(in: &cancellables)
        store.$enabledMetrics
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in self?.refreshTitle() }
            .store(in: &cancellables)
        store.$showRunner
            .receive(on: RunLoop.main)
            .sink { [weak self] show in
                guard let self else { return }
                item.button?.image = show ? PulseFrames.frame(frameIndex) : nil
                reschedule(cpu: store.sample?.cpuPct ?? 0)
            }
            .store(in: &cancellables)
        // Energy budget: stop animating while the display sleeps.
        let nc = NSWorkspace.shared.notificationCenter
        nc.addObserver(forName: NSWorkspace.screensDidSleepNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.paused = true; self?.timer?.invalidate() }
        }
        nc.addObserver(forName: NSWorkspace.screensDidWakeNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.paused = false; self?.reschedule(cpu: self?.store.sample?.cpuPct ?? 0) }
        }
        reschedule(cpu: 0)
    }

    private func refreshTitle() {
        let text = MenuBarText.build(sample: store.sample, enabled: store.enabledMetrics)
        item.button?.title = text.isEmpty ? (store.sample?.wattsLabel ?? "—W") : text
    }

    private func reschedule(cpu: Double) {
        timer?.invalidate()
        guard !paused, store.showRunner else { return }
        // Animation cadence is decoupled from the 2s sample cadence:
        // frames interpolate the latest known load.
        timer = Timer.scheduledTimer(withTimeInterval: RunnerCadence.frameInterval(cpuPct: cpu),
                                     repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                // Do not burn frames on an occluded menu bar
                // (fullscreen app on another Space, hidden bar).
                if let win = self.item.button?.window,
                   !win.occlusionState.contains(.visible) { return }
                self.frameIndex += 1
                self.item.button?.image = PulseFrames.frame(self.frameIndex)
            }
        }
    }

    @objc private func togglePopover() {
        guard let button = item.button else { return }
        if popover.isShown {
            popover.performClose(nil)
        } else {
            // PopoverView.onAppear is the single refresh trigger.
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        }
    }
}

/// The notch/pill overlay: a borderless, non-activating panel above
/// the menu bar, top-center of the notched (or main) screen. Public
/// APIs only — no private SkyLight/CGSSpace tricks.
///
/// The window is FIXED size (the expanded envelope plus shadow
/// room) and never resized; SwiftUI springs animate the silhouette
/// inside it. A fixed transparent window would normally eat clicks
/// across its whole frame, so a cursor poll flips
/// `ignoresMouseEvents` based on the visible shape's reported
/// frame: events are captured only while the cursor is actually
/// over the black panel. (Same approach as Glance, MIT.)
@MainActor
final class NotchPanelController {
    private let store: Store
    private var panel: NSPanel?
    private var cancellables: Set<AnyCancellable> = []
    private var visibleRect: CGRect? // in the root view's top-down coords
    private var cursorTimer: Timer?

    init(store: Store) {
        self.store = store
        NotificationCenter.default.addObserver(
            forName: NSApplication.didChangeScreenParametersNotification,
            object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.rebuild() }
        }
        store.$showNotchPanel
            .dropFirst()
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in self?.rebuild() }
            .store(in: &cancellables)
        rebuild()
    }

    static func geometry(for screen: NSScreen) -> NotchGeometry {
        if let sim = NotchGeometry.simulated(
            from: UserDefaults.standard.string(forKey: "MiniPulseSimulateNotch")) {
            return sim
        }
        return NotchGeometry(
            screenWidth: Double(screen.frame.width),
            safeAreaTop: Double(screen.safeAreaInsets.top),
            auxLeftWidth: screen.auxiliaryTopLeftArea.map { Double($0.width) },
            auxRightWidth: screen.auxiliaryTopRightArea.map { Double($0.width) })
    }

    /// Prefer the screen that actually has a notch (focus moving
    /// to an external display changes NSScreen.main without any
    /// screen-parameters notification); fall back to main.
    static func targetScreen() -> NSScreen? {
        NSScreen.screens.first { geometry(for: $0).hasNotch } ?? NSScreen.main
    }

    private func rebuild() {
        panel?.close()
        panel = nil
        cursorTimer?.invalidate()
        cursorTimer = nil
        visibleRect = nil
        guard store.showNotchPanel, let screen = Self.targetScreen() else { return }
        let geo = Self.geometry(for: screen)
        let size = geo.windowSize
        let frame = NSRect(x: screen.frame.midX - size.width / 2,
                           y: screen.frame.maxY - size.height,
                           width: size.width, height: size.height)
        let p = NSPanel(contentRect: frame,
                        styleMask: [.borderless, .nonactivatingPanel],
                        backing: .buffered, defer: false)
        p.isFloatingPanel = true
        p.level = NSWindow.Level(rawValue: NSWindow.Level.mainMenu.rawValue + 1)
        p.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
        p.backgroundColor = .clear
        p.isOpaque = false
        p.hasShadow = false
        p.isMovable = false
        p.hidesOnDeactivate = false
        p.animationBehavior = .none
        // Passthrough until the cursor is over the visible shape.
        p.ignoresMouseEvents = true
        // Root view pins itself to the full fixed window size, so
        // the hosting view's fitting size equals the window size
        // and cannot drift it.
        p.contentView = NSHostingView(rootView: NotchView(store: store, geometry: geo) { [weak self] rect in
            self?.visibleRect = rect
        })
        p.orderFrontRegardless()
        panel = p
        // Polling, not mouse-moved events: a window that ignores
        // mouse events receives none.
        let timer = Timer(timeInterval: 1.0 / 30.0, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.updatePassthrough() }
        }
        RunLoop.main.add(timer, forMode: .common)
        cursorTimer = timer
    }

    private func updatePassthrough() {
        guard let panel, let rect = visibleRect else { return }
        // rect is top-down within the window; convert to screen
        // coords (AppKit origin is bottom-left).
        let screenRect = NSRect(
            x: panel.frame.minX + rect.minX,
            y: panel.frame.maxY - rect.maxY,
            width: rect.width, height: rect.height)
        let over = screenRect.insetBy(dx: -6, dy: -6).contains(NSEvent.mouseLocation)
        let shouldIgnore = !over
        if panel.ignoresMouseEvents != shouldIgnore {
            panel.ignoresMouseEvents = shouldIgnore
        }
    }
}

@MainActor
final class PrefsWindowController {
    private var window: NSWindow?
    private let store: Store
    init(store: Store) { self.store = store }

    func show() {
        if window == nil {
            let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 420, height: 240),
                             styleMask: [.titled, .closable], backing: .buffered, defer: false)
            w.title = "MiniPulseNotch Preferences"
            w.contentViewController = NSHostingController(rootView: PrefsView(store: store))
            w.isReleasedWhenClosed = false
            w.center()
            window = w
        }
        NSApp.activate(ignoringOtherApps: true)
        window?.makeKeyAndOrderFront(nil)
    }
}
