import Foundation

/// Tiny persistent event log for diagnosing background behaviour. It survives
/// app relaunch (stored in UserDefaults), so callbacks that fire while the phone
/// is locked / the app is backgrounded are captured even if the process is later
/// killed and relaunched. Shipped to the backend on demand via HistoryClient.
enum EventLog {
    private static let key = "rp_event_log"
    private static let cap = 600                 // keep the newest N events
    private static let q = DispatchQueue(label: "net.roompulse.eventlog")

    /// Append one timestamped line. `detail` is free-form (we fold context like
    /// app state / locked / current room into it) to keep the wire shape simple.
    static func log(_ kind: String, _ detail: String = "") {
        let ts = Int64(Date().timeIntervalSince1970 * 1000)
        q.sync {
            let d = UserDefaults.standard
            var arr = d.array(forKey: key) as? [[String: Any]] ?? []
            arr.append(["ts": ts, "kind": kind, "detail": detail])
            if arr.count > cap { arr = Array(arr.suffix(cap)) }
            d.set(arr, forKey: key)
        }
    }

    static func snapshot() -> [[String: Any]] {
        q.sync { UserDefaults.standard.array(forKey: key) as? [[String: Any]] ?? [] }
    }

    static func clear() {
        q.sync { UserDefaults.standard.removeObject(forKey: key) }
    }
}
