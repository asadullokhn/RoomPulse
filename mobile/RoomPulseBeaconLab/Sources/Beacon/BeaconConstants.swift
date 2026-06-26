import Foundation

enum BeaconConstants {
    /// One UUID for the whole building/org. Both phones (and later the ESP32)
    /// share it. Major = floor/zone, Minor = room number.
    static let uuid = UUID(uuidString: "11111111-2222-3333-4444-555555555555")!

    /// Default backend base URL — the public TezTun tunnel, so the app works
    /// from anywhere with no manual setup. (Override in-app for local testing.)
    static let defaultBackendBaseURL = "https://room.teztun.uz"
}

/// A room's beacon identity, mapped to the backend's Zoom workspace id.
/// Fetched from the backend (`GET /beacons`) at runtime; `defaults` is only the
/// offline/first-launch fallback. Codable so the last fetched list survives a
/// kill (a region relaunch must monitor the right rooms with no network).
struct RoomPreset: Identifiable, Hashable, Codable {
    let name: String
    let major: UInt16
    let minor: UInt16
    let workspaceID: String

    var id: String { workspaceID }

    /// Built-in fallback, used only until the first successful `/beacons` fetch.
    static let defaults: [RoomPreset] = [
        RoomPreset(name: "Room A", major: 1, minor: 101, workspaceID: "ws-a"),
        RoomPreset(name: "Room B", major: 1, minor: 102, workspaceID: "ws-b"),
        RoomPreset(name: "Room C", major: 2, minor: 201, workspaceID: "ws-c"),
    ]
}
