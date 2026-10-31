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
final class StatusItemController: NSObject, NSPopoverDelegate {
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
        // Not .transient: an LSUIElement app never becomes the
        // active app, so transient's close-on-outside-interaction
        // never fires (and it fights the button toggle when it
        // does). Outside clicks are handled by monitors below.
        popover.behavior = .applicationDefined
        popover.delegate = self
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
            installClickMonitors()
        }
    }

    func popoverDidClose(_ notification: Notification) {
        removeClickMonitors()
    }

    // Dismiss on any click outside the popover (Esc too), like
    // every other menu-bar app. The status button is excluded:
    // its action toggles.
    private var clickMonitors: [Any] = []

    private func installClickMonitors() {
        removeClickMonitors()
        let global = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
            Task { @MainActor in
                guard let self, self.popover.isShown else { return }
                let p = NSEvent.mouseLocation
                if let b = self.buttonInScreenCoords, b.contains(p) { return }
                if let w = self.popover.contentViewController?.view.window, w.frame.contains(p) { return }
                self.popover.performClose(nil)
            }
        }
        let local = NSEvent.addLocalMonitorForEvents(matching: [.keyDown]) { [weak self] e in
            if e.keyCode == 53 { // Esc
                Task { @MainActor in self?.popover.performClose(nil) }
                return nil
            }
            return e
        }
        clickMonitors = [global, local].compactMap { $0 }
    }

    private func removeClickMonitors() {
        clickMonitors.forEach { NSEvent.removeMonitor($0) }
        clickMonitors = []
    }

    private var buttonInScreenCoords: NSRect? {
        guard let button = item.button, let win = button.window else { return nil }
        return win.convertToScreen(button.convert(button.bounds, to: nil))
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
            w.title = "MiniPulseMenu Preferences"
            w.contentViewController = NSHostingController(rootView: PrefsView(store: store))
            w.isReleasedWhenClosed = false
            w.center()
            window = w
        }
        NSApp.activate(ignoringOtherApps: true)
        window?.makeKeyAndOrderFront(nil)
    }
}
