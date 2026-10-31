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

    static func save(_ token: String) {
        let base: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                   kSecAttrService as String: service,
                                   kSecAttrAccount as String: account]
        SecItemDelete(base as CFDictionary)
        guard !token.isEmpty, let data = token.data(using: .utf8) else { return }
        var attrs = base
        attrs[kSecValueData as String] = data
        SecItemAdd(attrs as CFDictionary, nil)
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
    private var lastSampleAt: Date?
    private let session = URLSession(configuration: .ephemeral)

    init() {
        host = UserDefaults.standard.string(forKey: "host") ?? "http://127.0.0.1:8848"
        showNotchPanel = UserDefaults.standard.object(forKey: "showNotchPanel") as? Bool ?? true
    }

    var baseURL: URL? { URL(string: host) }
    var dashboardURL: URL? { baseURL }

    func start() {
        task?.cancel()
        task = Task { await runLoop() }
        Task { await staleWatcher() }
    }

    func restart() { state = .connecting; start() }

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
                backoff = 1
            } catch {
                if Task.isCancelled { return }
                state = sample == nil ? .offline("daemon unreachable") : .stale
                await resyncMini()
                try? await Task.sleep(nanoseconds: backoff * 1_000_000_000)
                backoff = min(backoff * 2, 15)
            }
        }
    }

    private func stream() async throws {
        guard let req = request("/api/stream") else {
            state = .offline("invalid host URL"); return
        }
        let (bytes, response) = try await session.bytes(for: req)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
            let code = (response as? HTTPURLResponse)?.statusCode ?? -1
            state = .offline(code == 401 ? "token missing/wrong" : "HTTP \(code)")
            throw URLError(.badServerResponse)
        }
        for try await line in bytes.lines {
            if let s = SSEParser.sample(fromLine: line) {
                sample = s
                lastSampleAt = Date()
                state = .live
            }
        }
        throw URLError(.networkConnectionLost)
    }

    private func resyncMini() async {
        guard let req = request("/api/mini?top=1") else { return }
        if let (data, _) = try? await session.data(for: req),
           let s = try? MiniPulseJSON.decodeMini(data) {
            sample = s
            lastSampleAt = Date()
        }
    }

    private func staleWatcher() async {
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if state == .live, let at = lastSampleAt, Date().timeIntervalSince(at) > 8 {
                state = .stale
            }
        }
    }

    func refreshTopProcesses() {
        Task {
            guard let req = request("/api/processes?sort=cpu&limit=5") else { return }
            if let (data, _) = try? await session.data(for: req),
               let rows = try? JSONDecoder().decode([ProcRow].self, from: data) {
                topProcesses = rows
            }
        }
    }
}
