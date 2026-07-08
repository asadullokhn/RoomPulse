import Foundation

/// Talks to the Mac-side TX tuner bridge (device/tools/txtuner.py), which
/// relays TX-power changes to the nRF52840 lab beacon over USB serial.
struct TunerState: Decodable {
    let major: Int
    let minor: Int
    let tx: Int
    let adv: Int
    let uuid: String?
}

enum TunerError: LocalizedError {
    case transport(String)
    case server(String)

    var errorDescription: String? {
        switch self {
        case .transport(let s), .server(let s): return s
        }
    }
}

enum TunerClient {
    /// TX levels the nRF52840 SoftDevice accepts — must match the firmware and txtuner.py.
    static let levels = [-40, -20, -16, -12, -8, -4, 0, 2, 3, 4, 5, 6, 7, 8]

    static func fetchState(completion: @escaping (Result<TunerState, TunerError>) -> Void) {
        guard let url = URL(string: AppSettings.tunerBaseURL + "/api/state") else {
            completion(.failure(.transport("Bad tuner URL")))
            return
        }
        var req = URLRequest(url: url)
        req.timeoutInterval = 8
        run(req, completion: completion)
    }

    static func setTx(level: Int, completion: @escaping (Result<TunerState, TunerError>) -> Void) {
        guard let url = URL(string: AppSettings.tunerBaseURL + "/api/tx") else {
            completion(.failure(.transport("Bad tuner URL")))
            return
        }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 8
        req.httpBody = try? JSONSerialization.data(withJSONObject: ["level": level])
        run(req, completion: completion)
    }

    /// Re-homes the beacon to another room: sets major and/or minor (1..65535)
    /// over the Mac bridge's POST /api/id. Serial-only — the backend mapping
    /// is untouched.
    static func setIdent(major: Int?, minor: Int?, completion: @escaping (Result<TunerState, TunerError>) -> Void) {
        guard let url = URL(string: AppSettings.tunerBaseURL + "/api/id") else {
            completion(.failure(.transport("Bad tuner URL")))
            return
        }
        var body: [String: Int] = [:]
        if let major { body["major"] = major }
        if let minor { body["minor"] = minor }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 8
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        run(req, completion: completion)
    }

    private static func run(_ req: URLRequest,
                            completion: @escaping (Result<TunerState, TunerError>) -> Void) {
        URLSession.shared.dataTask(with: req) { data, resp, err in
            let result: Result<TunerState, TunerError>
            if let err {
                result = .failure(.transport("Mac unreachable - is txtuner.py running? (\(err.localizedDescription))"))
            } else if let http = resp as? HTTPURLResponse, let data {
                if (200..<300).contains(http.statusCode),
                   let state = try? JSONDecoder().decode(TunerState.self, from: data) {
                    result = .success(state)
                } else if let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                          let msg = obj["error"] as? String {
                    result = .failure(.server(msg))
                } else {
                    result = .failure(.server("HTTP \(http.statusCode)"))
                }
            } else {
                result = .failure(.transport("No response"))
            }
            DispatchQueue.main.async { completion(result) }
        }.resume()
    }
}
