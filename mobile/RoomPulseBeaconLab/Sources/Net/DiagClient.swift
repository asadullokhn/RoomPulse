import Foundation

/// Ships the device's current diagnostics snapshot to the backend (POST /diag)
/// so it can be inspected remotely (GET /diag) when the phone isn't in hand.
/// Fire-and-forget, like the presence heartbeat.
enum DiagClient {
    static func send(deviceID: String,
                     displayName: String,
                     summary: String,
                     ready: Bool,
                     line: String,
                     rows: [[String: String]],
                     completion: @escaping (Bool) -> Void) {
        guard let url = URL(string: AppSettings.backendBaseURL + "/diag") else {
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
            "summary": summary,
            "ready": ready,
            "line": line,
            "rows": rows,
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        URLSession.shared.dataTask(with: req) { _, resp, _ in
            let ok = (resp as? HTTPURLResponse).map { (200..<300).contains($0.statusCode) } ?? false
            DispatchQueue.main.async { completion(ok) }
        }.resume()
    }
}
