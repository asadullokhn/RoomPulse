import Foundation

/// Reports the device's CURRENT room state to the backend (idempotent
/// reconciliation, not deltas — a lost message is corrected by the next one).
/// Stateless so it works from a background relaunch.
enum PresenceClient {
    /// workspaceID == "" means "not in any room".
    static func heartbeat(deviceID: String,
                          displayName: String,
                          workspaceID: String,
                          eventTS: Int64,
                          completion: @escaping (Bool) -> Void) {
        guard let url = URL(string: AppSettings.backendBaseURL + "/presence/heartbeat") else {
            completion(false)
            return
        }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 8
        let body: [String: Any] = [
            "device_id": deviceID,
            "display_name": displayName,
            "workspace_id": workspaceID,   // "" = not in any room
            "ts": eventTS,                 // epoch millis (last-write-wins per device)
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        URLSession.shared.dataTask(with: req) { _, resp, _ in
            let ok = (resp as? HTTPURLResponse).map { (200..<300).contains($0.statusCode) } ?? false
            completion(ok)
        }.resume()
    }
}
