import Foundation

/// Ships the full persistent event log to the backend (POST /history) on demand,
/// so the background behaviour can be reviewed remotely (GET /history?format=text).
enum HistoryClient {
    static func send(deviceID: String,
                     name: String,
                     events: [[String: Any]],
                     completion: @escaping (Bool) -> Void) {
        guard let url = URL(string: AppSettings.backendBaseURL + "/history") else {
            completion(false)
            return
        }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 15
        let body: [String: Any] = ["device_id": deviceID, "name": name, "events": events]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        URLSession.shared.dataTask(with: req) { _, resp, _ in
            let ok = (resp as? HTTPURLResponse).map { (200..<300).contains($0.statusCode) } ?? false
            DispatchQueue.main.async { completion(ok) }
        }.resume()
    }
}
