import Foundation
import Security
import MiniPulseCore

enum ConnectionState: Equatable {
    case connecting
    case live
    case stale          // connected once, no fresh sample lately
    case offline(String)
}

struct ProcRow: Decodable, Identifiable {
    var pid: Int32; var name: String; var cpu_pct: Double; var mem_pct: Double
    var id: Int32 { pid }
}

/// API token storage: Keychain only, never UserDefaults or logs.
enum TokenStore {
    private static let service = "io.github.ricardotech.minipulse.notch"
    private static let account = "api-token"

    static func load() -> String {
        var query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                    kSecAttrService as String: service,
                                    kSecAttrAccount as String: account,
                                    kSecReturnData as String: true,
                                    kSecMatchLimit as String: kSecMatchLimitOne]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data else { return "" }
        query.removeAll()
        return String(data: data, encoding: .utf8) ?? ""
    }

    /// Returns false when the Keychain refused (e.g. ad-hoc rebuild
    /// changed the app identity) so Preferences can say so instead
    /// of silently looping on 401s.
    @discardableResult
    static func save(_ token: String) -> Bool {
        let base: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                   kSecAttrService as String: service,
                                   kSecAttrAccount as String: account]
        SecItemDelete(base as CFDictionary)
        guard !token.isEmpty, let data = token.data(using: .utf8) else { return true }
        var attrs = base
        attrs[kSecValueData as String] = data
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        return SecItemAdd(attrs as CFDictionary, nil) == errSecSuccess
    }
}

/// Refuses every redirect: URLSession would otherwise re-send the
/// X-MiniPulse-Token header to the redirect target, possibly on a
/// different origin controlled by whoever answered first.
final class RedirectRefuser: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest,
                    completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

@MainActor
final class Store: ObservableObject {
    @Published var sample: MiniSample?
    @Published var state: ConnectionState = .connecting
    @Published var topProcesses: [ProcRow] = []
    @Published var notchExpanded = false

    @Published var host: String {
        didSet { UserDefaults.standard.set(host, forKey: "host") }
    }
    @Published var showNotchPanel: Bool {
        didSet { UserDefaults.standard.set(showNotchPanel, forKey: "showNotchPanel") }
    }

    private var task: Task<Void, Never>?
    private var watcherTask: Task<Void, Never>?
    private var topTask: Task<Void, Never>?
    private var lastSampleAt: Date?
    private let redirectRefuser = RedirectRefuser()
    private lazy var session = URLSession(configuration: .ephemeral,
                                          delegate: redirectRefuser, delegateQueue: nil)

    init() {
        host = UserDefaults.standard.string(forKey: "host") ?? "http://127.0.0.1:8848"
        showNotchPanel = UserDefaults.standard.object(forKey: "showNotchPanel") as? Bool ?? true
    }

    var baseURL: URL? { URL(string: host) }
    var dashboardURL: URL? { baseURL }

    func start() {
        task?.cancel()
        task = Task { await runLoop() }
        // Exactly one watcher for the process lifetime; restarting
        // the connection must not stack immortal 2s tickers.
        if watcherTask == nil {
            watcherTask = Task { await staleWatcher() }
        }
    }

    func restart() {
        state = .connecting
        topProcesses = [] // never show the previous host's processes
        start()
    }

    private func request(_ path: String) -> URLRequest? {
        guard let base = baseURL, let url = URL(string: path, relativeTo: base) else { return nil }
        var req = URLRequest(url: url)
        let token = TokenStore.load()
        if !token.isEmpty { req.setValue(token, forHTTPHeaderField: "X-MiniPulse-Token") }
        return req
    }

    /// SSE primary (server-paced, one event per 2s sample); on any
    /// failure, one /api/mini resync, backoff, and retry. No polling
    /// of /api/overview (it runs a store query per call).
    private func runLoop() async {
        var backoff: UInt64 = 1
        while !Task.isCancelled {
            do {
                try await stream()
            } catch {
                if Task.isCancelled { return }
                // Keep a specific reason set by stream() (401,
                // invalid host…); only fall back to a generic one.
                if case .offline = state {
                    // keep stream()'s reason
                } else {
                    state = sample == nil ? .offline("daemon unreachable") : .stale
                }
                await resyncMini()
                // A session that delivered fresh samples recently
                // was healthy: next retry starts from 1s again.
                if let at = lastSampleAt, Date().timeIntervalSince(at) < 15 {
                    backoff = 1
                }
                try? await Task.sleep(nanoseconds: backoff * 1_000_000_000)
                backoff = min(backoff * 2, 15)
            }
        }
    }

    private func stream() async throws {
        // Invalid host must THROW so runLoop's backoff applies;
        // returning normally here spun the loop (and the Keychain)
        // as fast as the CPU allowed.
        if case .invalid(let why) = HostValidation.validate(host) {
            state = .offline("invalid host: \(why)")
            throw URLError(.badURL)
        }
        guard let req = request("/api/stream") else {
            state = .offline("invalid host URL")
            throw URLError(.badURL)
        }
        let (bytes, response) = try await session.bytes(for: req)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
            let code = (response as? HTTPURLResponse)?.statusCode ?? -1
            state = .offline(code == 401 ? "token missing/wrong" : "HTTP \(code)")
            throw URLError(.badServerResponse)
        }
        for try await line in bytes.lines {
            if let s = SSEParser.sample(fromLine: line) {
                accept(s)
            }
        }
        throw URLError(.networkConnectionLost)
    }

    /// Freshness is judged by the sample's own collection time
    /// when the daemon sent one, never just by arrival.
    private func accept(_ s: MiniSample) {
        sample = s
        let collectedAt = s.time ?? Date()
        lastSampleAt = collectedAt
        state = Date().timeIntervalSince(collectedAt) > 15 ? .stale : .live
    }

    private func resyncMini() async {
        guard let req = request("/api/mini?top=1") else { return }
        if let (data, _) = try? await session.data(for: req),
           let s = try? MiniPulseJSON.decodeMini(data) {
            accept(s)
        }
    }

    private func staleWatcher() async {
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if state == .live, let at = lastSampleAt, Date().timeIntervalSince(at) > 15 {
                state = .stale
            }
        }
    }

    func refreshTopProcesses() {
        topTask?.cancel()
        topTask = Task {
            guard let req = request("/api/processes?sort=cpu&limit=5") else {
                topProcesses = []; return
            }
            if let (data, _) = try? await session.data(for: req),
               let rows = try? JSONDecoder().decode([ProcRow].self, from: data) {
                if !Task.isCancelled { topProcesses = rows }
            } else if !Task.isCancelled {
                topProcesses = [] // stale rows are worse than none
            }
        }
    }
}
