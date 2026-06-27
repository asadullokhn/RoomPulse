import Foundation

/// Persisted settings the background monitor reads even after the app was killed.
enum AppSettings {
    private static let d = UserDefaults.standard

    static var backendBaseURL: String {
        get { d.string(forKey: "backendBaseURL") ?? BeaconConstants.defaultBackendBaseURL }
        set { d.set(newValue, forKey: "backendBaseURL") }
    }

    /// Display name (editable). Shown in the dashboard; NOT the presence key.
    static var userID: String {
        get { d.string(forKey: "userID") ?? "user-1" }
        set { d.set(newValue, forKey: "userID") }
    }

    /// Stable per-install presence key. Generated once and never changes, so a
    /// check-in and its check-out always match even if the name is edited.
    static var deviceID: String {
        if let v = d.string(forKey: "deviceID") { return v }
        let v = "dev-" + UUID().uuidString.prefix(8)
        d.set(String(v), forKey: "deviceID")
        return String(v)
    }

    /// Minimum RSSI (dBm) to check in. Higher (e.g. -55) = must be very close;
    /// lower (e.g. -80) = checks in from farther. RSSI is always negative, so a
    /// stored 0 means "unset" → default.
    static var rssiThreshold: Int {
        get { let v = d.integer(forKey: "rssiThreshold"); return v == 0 ? -65 : v }
        set { d.set(newValue, forKey: "rssiThreshold") }
    }

    /// Local notifications on auto check-in / check-out. Default OFF (bool(forKey:)
    /// returns false when unset) — presence still works silently.
    static var notifyOnCheckInOut: Bool {
        get { d.bool(forKey: "notifyOnCheckInOut") }
        set { d.set(newValue, forKey: "notifyOnCheckInOut") }
    }
}
