import AppKit

// Agent app (LSUIElement in the bundle): no Dock icon, lives in the
// menu bar + notch. The Go daemon remains the only collector.
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var store: Store!
    private var statusItem: StatusItemController!
    private var notchPanel: NotchPanelController!
    private var prefs: PrefsWindowController!

    func applicationDidFinishLaunching(_ notification: Notification) {
        store = Store()
        prefs = PrefsWindowController(store: store)
        statusItem = StatusItemController(store: store)
        statusItem.openPrefs = { [weak self] in self?.prefs.show() }
        notchPanel = NotchPanelController(store: store)
        store.start()
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = MainActor.assumeIsolated { AppDelegate() }
app.delegate = delegate
app.run()
