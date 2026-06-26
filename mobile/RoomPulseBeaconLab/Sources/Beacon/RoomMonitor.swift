import Foundation
import CoreLocation
import UserNotifications
import UIKit

/// A beacon currently being ranged (live signal shown on the main page).
struct RangedBeacon: Identifiable {
    let id: String
    let room: String
    let proximity: String
    let accuracy: Double
    let rssi: Int
}

/// Tracks which single room you're in and reports it to the backend as an
/// idempotent heartbeat (current state, not deltas), so a lost message can't
/// strand you "in" a room — the next 3s heartbeat reconciles.
///
/// - Foreground: ranging decides the room (RSSI ≥ threshold), a 3s timer sends
///   the current state and applies a grace-based leave.
/// - Background / closed: region monitoring sets the state on enter/exit
///   (ranging isn't delivered when closed).
final class RoomMonitor: NSObject, ObservableObject {
    static let shared = RoomMonitor()

    @Published var isMonitoring = false
    @Published var statusText = "Off"
    @Published var lastEvent = ""
    @Published var insideRooms: Set<String> = []   // 0 or 1 element (current room), for the UI
    @Published var liveBeacons: [RangedBeacon] = []

    private let manager = CLLocationManager()
    private lazy var constraint = CLBeaconIdentityConstraint(uuid: BeaconConstants.uuid)

    private var currentRoom: String?               // the one room we're in (source of truth)
    private var nearHits: [String: Int] = [:]      // room -> consecutive near samples
    private var lastSeenNear: [String: Date] = [:] // room -> last time genuinely near
    private var tickTimer: Timer?
    private var beaconTimer: Timer?                 // periodic /beacons refresh
    private var lastSentAt = Date.distantPast

    private static let curKey = "rp_current_room"
    private static let enterThreshold = 2               // ~2s sustained near before check-in
    private static let exitGrace: TimeInterval = 6      // leave after 6s of not being near
    private static let tickInterval: TimeInterval = 2   // local grace/keepalive check (no network)
    private static let keepaliveInterval: TimeInterval = 45 // network keepalive cadence (< backend TTL)
    private static let beaconRefreshInterval: TimeInterval = 300 // re-fetch the beacon list every 5 min

    private override init() {
        super.init()
        manager.delegate = self
        currentRoom = UserDefaults.standard.string(forKey: Self.curKey)
        insideRooms = currentRoom.map { [$0] } ?? []
        // Scope the high-power ranging scan to the foreground only.
        NotificationCenter.default.addObserver(self, selector: #selector(appDidBackground),
                                               name: UIApplication.didEnterBackgroundNotification, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(appWillForeground),
                                               name: UIApplication.willEnterForegroundNotification, object: nil)
        if !manager.monitoredRegions.isEmpty {
            isMonitoring = true
            statusText = "Monitoring \(manager.monitoredRegions.count) rooms"
        }
    }

    // Ranging is HIGH power; region monitoring is low power and runs the whole
    // time. So range only while foregrounded; let region events drive presence
    // when backgrounded.
    @objc private func appDidBackground() {
        guard isMonitoring else { return }
        manager.stopRangingBeacons(satisfying: constraint)
        tickTimer?.invalidate()
        tickTimer = nil
    }

    @objc private func appWillForeground() {
        guard isMonitoring else { return }
        manager.startRangingBeacons(satisfying: constraint)
        startTick()
        refreshBeacons() // catch any room/beacon changes since we backgrounded
    }

    func bootstrap() { _ = manager }

    func enableBackgroundCheckIn() {
        manager.requestAlwaysAuthorization()
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
        startMonitoringRooms()
    }

    func disable() {
        for region in manager.monitoredRegions { manager.stopMonitoring(for: region) }
        manager.stopRangingBeacons(satisfying: constraint)
        manager.allowsBackgroundLocationUpdates = false
        tickTimer?.invalidate()
        tickTimer = nil
        beaconTimer?.invalidate()
        beaconTimer = nil
        if currentRoom != nil { setCurrentRoom(nil) }   // tell backend we're gone
        isMonitoring = false
        statusText = "Off"
        liveBeacons.removeAll()
        nearHits.removeAll()
        lastSeenNear.removeAll()
    }

    private func startMonitoringRooms() {
        for room in RoomRegistry.shared.rooms {
            manager.startMonitoring(for: monitorRegion(for: room))
        }
        manager.startRangingBeacons(satisfying: constraint)
        startTick()
        startBeaconRefresh()
        refreshBeacons() // pull the latest list right away
        isMonitoring = true
        statusText = "Auto check-in when near · check-out when you leave"
    }

    private func monitorRegion(for room: RoomPreset) -> CLBeaconRegion {
        let region = CLBeaconRegion(
            uuid: BeaconConstants.uuid,
            major: room.major,
            minor: room.minor,
            identifier: room.workspaceID
        )
        region.notifyOnEntry = true
        region.notifyOnExit = true
        region.notifyEntryStateOnDisplay = true
        return region
    }

    private func startTick() {
        tickTimer?.invalidate()
        let t = Timer(timeInterval: Self.tickInterval, repeats: true) { [weak self] _ in self?.tick() }
        RunLoop.main.add(t, forMode: .common)
        tickTimer = t
    }

    // MARK: - beacon list refresh
    private func startBeaconRefresh() {
        beaconTimer?.invalidate()
        let t = Timer(timeInterval: Self.beaconRefreshInterval, repeats: true) { [weak self] _ in
            self?.refreshBeacons()
        }
        RunLoop.main.add(t, forMode: .common)
        beaconTimer = t
    }

    /// Fetch the backend beacon list; if it changed, re-register the monitored
    /// regions so newly added/re-mapped rooms take effect without a new build.
    func refreshBeacons() {
        BeaconClient.fetch { [weak self] rooms in
            guard let self, let rooms, RoomRegistry.shared.update(rooms) else { return }
            DispatchQueue.main.async {
                guard self.isMonitoring else { return }
                for region in self.manager.monitoredRegions { self.manager.stopMonitoring(for: region) }
                for room in RoomRegistry.shared.rooms {
                    self.manager.startMonitoring(for: self.monitorRegion(for: room))
                }
                self.statusText = "Monitoring \(RoomRegistry.shared.rooms.count) rooms"
                self.lastEvent = "beacon list updated"
            }
        }
    }

    // MARK: - lookups
    private func room(for region: CLRegion) -> RoomPreset? {
        RoomRegistry.shared.rooms.first { $0.workspaceID == region.identifier }
    }
    private func room(major: Int, minor: Int) -> RoomPreset? {
        RoomRegistry.shared.rooms.first { Int($0.major) == major && Int($0.minor) == minor }
    }
    private func room(named name: String) -> RoomPreset? {
        RoomRegistry.shared.rooms.first { $0.name == name }
    }
    private static func proximityText(_ p: CLProximity) -> String {
        switch p {
        case .immediate: return "Immediate"
        case .near: return "Near"
        case .far: return "Far"
        default: return "Unknown"
        }
    }

    // MARK: - state
    /// Sets the single current room (or nil for none). Idempotent: fires a local
    /// notification on change and pushes the new state to the backend.
    private func setCurrentRoom(_ name: String?) {
        guard currentRoom != name else { return }
        let old = currentRoom
        currentRoom = name
        insideRooms = name.map { [$0] } ?? []
        UserDefaults.standard.set(name, forKey: Self.curKey)
        if let n = name {
            notify(entered: true, room: n)
            lastEvent = "entered · \(n)"
        } else if let o = old {
            notify(entered: false, room: o)
            lastEvent = "left · \(o)"
        }
        sendState()
    }

    /// Foreground tick (local, no network): grace-based leave + a low-cadence
    /// keepalive. We POST on every state change (setCurrentRoom); the keepalive
    /// only refreshes the backend TTL while genuinely in a room — so the radio
    /// tail demotes between sends instead of being pinned by a 3s trickle.
    private func tick() {
        if let cur = currentRoom,
           Date().timeIntervalSince(lastSeenNear[cur] ?? .distantPast) > Self.exitGrace {
            setCurrentRoom(nil)   // sendState() runs inside on change
            return
        }
        if currentRoom != nil, Date().timeIntervalSince(lastSentAt) > Self.keepaliveInterval {
            sendState()
        }
    }

    /// Idempotent full-state report — robust to lost messages.
    private func sendState() {
        lastSentAt = Date()
        let ws = currentRoom.flatMap { room(named: $0)?.workspaceID } ?? ""
        let ts = Int64(Date().timeIntervalSince1970 * 1000)
        let bg = UIApplication.shared.beginBackgroundTask(withName: "heartbeat")
        PresenceClient.heartbeat(deviceID: AppSettings.deviceID,
                                 displayName: AppSettings.userID,
                                 workspaceID: ws,
                                 eventTS: ts) { _ in
            UIApplication.shared.endBackgroundTask(bg)
        }
    }

    private func notify(entered: Bool, room: String) {
        let content = UNMutableNotificationContent()
        if entered {
            content.title = "You're at \(room)"
            content.body = "Checked you in."
        } else {
            content.title = "You left \(room)"
            content.body = "Checked you out and released the room."
        }
        content.sound = .default
        UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
    }
}

extension RoomMonitor: CLLocationManagerDelegate {
    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        switch manager.authorizationStatus {
        case .authorizedAlways:
            manager.allowsBackgroundLocationUpdates = true
            if manager.monitoredRegions.isEmpty { startMonitoringRooms() }
        case .authorizedWhenInUse:
            statusText = "Tap again and choose 'Always' for closed-app check-in"
            if manager.monitoredRegions.isEmpty { startMonitoringRooms() }
        case .denied, .restricted:
            statusText = "Location denied — enable Always in Settings"
        default:
            break
        }
    }

    /// FOREGROUND authority: pick the single nearest qualifying room (RSSI ≥
    /// threshold, ignoring 0 dBm), require enterThreshold samples to check in.
    func locationManager(_ manager: CLLocationManager,
                         didRange beacons: [CLBeacon],
                         satisfying constraint: CLBeaconIdentityConstraint) {
        let enterRSSI = AppSettings.rssiThreshold
        var bestRoom: String?
        var bestRSSI = Int.min
        var live: [RangedBeacon] = []
        for b in beacons {
            let preset = room(major: b.major.intValue, minor: b.minor.intValue)
            let name = preset?.name ?? "Room \(b.major)/\(b.minor)"
            live.append(RangedBeacon(id: "\(b.major).\(b.minor)", room: name,
                                     proximity: Self.proximityText(b.proximity),
                                     accuracy: b.accuracy, rssi: b.rssi))
            if let preset, b.rssi != 0, b.rssi >= enterRSSI, b.rssi > bestRSSI {
                bestRSSI = b.rssi
                bestRoom = preset.name
            }
        }
        liveBeacons = live.sorted { $0.room < $1.room }

        if let r = bestRoom {
            lastSeenNear[r] = Date()
            nearHits[r, default: 0] += 1
            for k in Array(nearHits.keys) where k != r { nearHits[k] = 0 }
            if nearHits[r, default: 0] >= Self.enterThreshold {
                setCurrentRoom(r)
            }
        } else {
            nearHits.removeAll()
            // Leaving is handled by the heartbeat grace timer, not here, so a
            // single dropped sample doesn't bounce you out.
        }
    }

    /// BACKGROUND authority: region monitoring (ranging isn't delivered closed).
    func locationManager(_ manager: CLLocationManager, didEnterRegion region: CLRegion) {
        guard let room = room(for: region) else { return }
        if UIApplication.shared.applicationState != .active { setCurrentRoom(room.name) }
    }

    func locationManager(_ manager: CLLocationManager, didExitRegion region: CLRegion) {
        guard let room = room(for: region) else { return }
        if UIApplication.shared.applicationState != .active, currentRoom == room.name {
            setCurrentRoom(nil)
        }
    }

    func locationManager(_ manager: CLLocationManager, didDetermineState state: CLRegionState, for region: CLRegion) {
        guard state == .inside, let room = room(for: region) else { return }
        if UIApplication.shared.applicationState != .active { setCurrentRoom(room.name) }
    }
}
