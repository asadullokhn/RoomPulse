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
/// Keep `workspaceID` values matching your backend seed.json (ws-a/ws-b/ws-c).
struct RoomPreset: Identifiable, Hashable {
    let id = UUID()
    let name: String
    let major: UInt16
    let minor: UInt16
    let workspaceID: String

    static let all: [RoomPreset] = [
        RoomPreset(name: "Room A", major: 1, minor: 101, workspaceID: "ws-a"),
        RoomPreset(name: "Room B", major: 1, minor: 102, workspaceID: "ws-b"),
        RoomPreset(name: "Room C", major: 2, minor: 201, workspaceID: "ws-c"),
    ]
}
