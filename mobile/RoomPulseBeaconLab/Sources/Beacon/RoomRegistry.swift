import Foundation

/// The live room↔beacon list. Loaded from the backend (`GET /beacons`) and
/// cached in UserDefaults so a background relaunch (region event, app killed)
/// still knows which rooms to monitor before any network call returns.
final class RoomRegistry: ObservableObject {
    static let shared = RoomRegistry()

    @Published private(set) var rooms: [RoomPreset]

    private static let key = "rp_rooms"

    private init() {
        rooms = Self.loadCached() ?? RoomPreset.defaults
    }

    /// Replaces the list if it actually changed. Returns whether it changed, so
    /// the monitor only re-registers CoreLocation regions when needed. Safe to
    /// call from any thread — publishing is hopped to main.
    @discardableResult
    func update(_ newRooms: [RoomPreset]) -> Bool {
        let sorted = newRooms.sorted { $0.workspaceID < $1.workspaceID }
        guard sorted != rooms else { return false }
        if Thread.isMainThread {
            rooms = sorted
        } else {
            DispatchQueue.main.sync { self.rooms = sorted }
        }
        Self.saveCached(sorted)
        return true
    }

    private static func loadCached() -> [RoomPreset]? {
        guard let data = UserDefaults.standard.data(forKey: key),
              let decoded = try? JSONDecoder().decode([RoomPreset].self, from: data),
              !decoded.isEmpty else { return nil }
        return decoded
    }

    private static func saveCached(_ rooms: [RoomPreset]) {
        if let data = try? JSONEncoder().encode(rooms) {
            UserDefaults.standard.set(data, forKey: key)
        }
    }
}
