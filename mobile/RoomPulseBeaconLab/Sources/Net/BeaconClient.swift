import Foundation

/// Fetches the room↔iBeacon registry from the backend so the app learns which
/// beacons to range/monitor without shipping a new build.
enum BeaconClient {
    private struct Envelope: Decodable { let beacons: [Entry] }
    private struct Entry: Decodable {
        let workspace_id: String
        let name: String
        let major: Int
        let minor: Int
    }

    /// GET /beacons. Returns nil on any failure so the caller keeps the cached list.
    static func fetch(completion: @escaping ([RoomPreset]?) -> Void) {
        guard let url = URL(string: AppSettings.backendBaseURL + "/beacons") else {
            completion(nil)
            return
        }
        var req = URLRequest(url: url)
        req.timeoutInterval = 8
        URLSession.shared.dataTask(with: req) { data, resp, _ in
            guard let data,
                  let http = resp as? HTTPURLResponse, (200..<300).contains(http.statusCode),
                  let env = try? JSONDecoder().decode(Envelope.self, from: data) else {
                completion(nil)
                return
            }
            let rooms: [RoomPreset] = env.beacons.compactMap { e in
                guard (0...65535).contains(e.major), (0...65535).contains(e.minor),
                      !e.workspace_id.isEmpty else { return nil }
                let name = e.name.isEmpty ? "Room \(e.major)/\(e.minor)" : e.name
                return RoomPreset(name: name, major: UInt16(e.major), minor: UInt16(e.minor),
                                  workspaceID: e.workspace_id)
            }
            completion(rooms.isEmpty ? nil : rooms)
        }.resume()
    }
}
