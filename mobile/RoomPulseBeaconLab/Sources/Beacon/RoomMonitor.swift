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

/// Severity of a diagnostics row. The view maps this to a colour + SF Symbol;
/// the model stays free of any SwiftUI dependency.
enum DiagLevel {
    case ok, warn, bad, info

    /// Stable wire name sent to the backend.
    var name: String {
        switch self {
        case .ok:   return "ok"
        case .warn: return "warn"
        case .bad:  return "bad"
        case .info: return "info"
        }
    }
}

/// One line in the diagnostics panel: a labelled health check with an optional
/// one-line hint shown only when something needs the user's attention.
struct DiagRow: Identifiable {
    let id: String
    let label: String
    let value: String
    let level: DiagLevel
    let hint: String?
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
    @Published var needsAlwaysInSettings = false   // show a "open Settings" affordance
    @Published var diag = "—"                      // copy-friendly one-line readout (support)
    @Published var diagRows: [DiagRow] = []        // structured diagnostics for the UI
    @Published var diagReady = false               // all closed-app prerequisites met?
    @Published var diagSummary = "—"               // headline verdict for the panel

    private var askedAlways = false                // the one-shot system "Always" prompt

    private let manager = CLLocationManager()
    private lazy var constraint = CLBeaconIdentityConstraint(uuid: BeaconConstants.uuid)

    private var currentRoom: String?               // the one room we're in (source of truth)
    private var nearHits: [String: Int] = [:]      // room -> consecutive near samples
    private var lastSeenNear: [String: Date] = [:] // room -> last time genuinely near
    private var tickTimer: Timer?
    private var beaconTimer: Timer?                 // periodic /beacons refresh
    private var lastSentAt = Date.distantPast
    private var lastRangeLogAt = Date.distantPast   // throttle for the ranging-summary event log

    private static let regionSchemaKey = "rp_region_schema_v"
    private static let regionSchema = 2             // bump when monitorRegion() properties change

    private static let curKey = "rp_current_room"
    private static let enterThreshold = 2               // consecutive near samples before check-in (foreground)
    private static let exitGrace: TimeInterval = 6      // foreground: check out after this long not "near"
    private static let tickInterval: TimeInterval = 2   // foreground grace/keepalive cadence
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
        // Lock/unlock markers — the smoking gun for "unlocking fires a check-in".
        NotificationCenter.default.addObserver(self, selector: #selector(onUnlock),
                                               name: UIApplication.protectedDataDidBecomeAvailableNotification, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(onLock),
                                               name: UIApplication.protectedDataWillBecomeUnavailableNotification, object: nil)
        var rebuilt = false
        if !manager.monitoredRegions.isEmpty {
            isMonitoring = true
            statusText = "Monitoring \(manager.monitoredRegions.count) rooms"
            rebuilt = rebuildRegionsIfStale()
        }
        EventLog.log("app.launch", "regions=\(manager.monitoredRegions.count) auth=\(manager.authorizationStatus.rawValue) restoredRoom=\(currentRoom ?? "-") rebuilt=\(rebuilt)")
        updateDiag()
    }

    @objc private func onUnlock() { EventLog.log("unlock", appCtx()) }
    @objc private func onLock() { EventLog.log("lock", appCtx()) }

    /// iOS persists monitored regions across an app UPGRADE with the property
    /// values they had at first registration — so a changed monitorRegion() flag
    /// (e.g. notifyEntryStateOnDisplay) never takes effect until we re-register.
    /// Rebuild once per schema bump. Returns true if it rebuilt.
    @discardableResult
    private func rebuildRegionsIfStale() -> Bool {
        let d = UserDefaults.standard
        guard d.integer(forKey: Self.regionSchemaKey) != Self.regionSchema else { return false }
        let rooms = RoomRegistry.shared.rooms
        guard !rooms.isEmpty else { return false } // don't tear down with no replacements
        for region in manager.monitoredRegions { manager.stopMonitoring(for: region) }
        for room in rooms { manager.startMonitoring(for: monitorRegion(for: room)) }
        d.set(Self.regionSchema, forKey: Self.regionSchemaKey)
        EventLog.log("regions.rebuilt", "count=\(rooms.count)")
        return true
    }

    /// Compact context string folded into every logged event.
    private func appCtx() -> String {
        let st: String
        switch UIApplication.shared.applicationState {
        case .active: st = "active"
        case .inactive: st = "inactive"
        case .background: st = "bg"
        @unknown default: st = "?"
        }
        return "app=\(st) locked=\(!UIApplication.shared.isProtectedDataAvailable) cur=\(currentRoom ?? "-")"
    }

    // Region monitoring (low power, runs even when the app is closed) is the single
    // source of truth for check-in/out. Ranging is foreground-only and powers just
    // the live-signal meter — so backgrounding only stops ranging, never presence.
    @objc private func appDidBackground() {
        EventLog.log("app.background", appCtx())
        guard isMonitoring else { return }
        manager.stopRangingBeacons(satisfying: constraint)
        tickTimer?.invalidate()
        tickTimer = nil
    }

    @objc private func appWillForeground() {
        EventLog.log("app.foreground", appCtx())
        updateDiag()
        guard isMonitoring else { return }
        manager.startRangingBeacons(satisfying: constraint) // foreground: ranging drives state
        startTick()
        refreshBeacons() // catch any room/beacon changes since we backgrounded
    }

    func bootstrap() { _ = manager }

    func enableBackgroundCheckIn() {
        if AppSettings.notifyOnCheckInOut { requestNotificationAuthorization() }
        switch manager.authorizationStatus {
        case .notDetermined:
            // Ask for When-In-Use first; we escalate to Always on the grant
            // callback (the only reliable way iOS shows the Always dialog).
            manager.requestWhenInUseAuthorization()
        case .authorizedWhenInUse:
            if askedAlways {
                openSettings()
            } else {
                askedAlways = true
                manager.requestAlwaysAuthorization()
            }
            startMonitoringRooms()
        case .authorizedAlways:
            startMonitoringRooms()
        case .denied, .restricted:
            needsAlwaysInSettings = true
            statusText = "Location is off — enable it in Settings"
            openSettings()
        @unknown default:
            break
        }
    }

    /// Ask for notification permission — only called when the user turns on
    /// check-in/out notifications (off by default).
    func requestNotificationAuthorization() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }

    func openSettings() {
        if let url = URL(string: UIApplication.openSettingsURLString) {
            UIApplication.shared.open(url)
        }
    }

    func disable() {
        for region in manager.monitoredRegions { manager.stopMonitoring(for: region) }
        manager.stopRangingBeacons(satisfying: constraint)
        manager.allowsBackgroundLocationUpdates = false
        tickTimer?.invalidate()
        tickTimer = nil
        beaconTimer?.invalidate()
        beaconTimer = nil
        if currentRoom != nil { setCurrentRoom(nil, by: "disable") }   // tell backend we're gone
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
        NSLog("RPDEBUG monitoringStarted regions=\(manager.monitoredRegions.count) auth=\(manager.authorizationStatus.rawValue) accuracy=\(manager.accuracyAuthorization.rawValue) bgUpdates=\(manager.allowsBackgroundLocationUpdates)")
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
        // Was true — but that re-delivers "inside" every time you turn the screen
        // on (unlock), so you'd get a fresh check-in notification on each unlock
        // while still in range. Off: presence is driven by actually crossing the
        // region boundary (background) and by ranging (foreground), not screen wakes.
        region.notifyEntryStateOnDisplay = false
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
        RoomRegistry.shared.refresh { [weak self] changed in
            guard let self, changed, self.isMonitoring else { return }
            for region in self.manager.monitoredRegions { self.manager.stopMonitoring(for: region) }
            for room in RoomRegistry.shared.rooms {
                self.manager.startMonitoring(for: self.monitorRegion(for: room))
            }
            self.statusText = "Monitoring \(RoomRegistry.shared.rooms.count) rooms"
            self.lastEvent = "beacon list updated"
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
    private func setCurrentRoom(_ name: String?, by caller: String = "?") {
        guard currentRoom != name else { return }
        let old = currentRoom
        currentRoom = name
        insideRooms = name.map { [$0] } ?? []
        UserDefaults.standard.set(name, forKey: Self.curKey)
        EventLog.log("setRoom", "from=\(old ?? "-") to=\(name ?? "-") by=\(caller) \(appCtx())")
        if let n = name {
            notify(entered: true, room: n)
            lastEvent = "entered · \(n)"
        } else if let o = old {
            notify(entered: false, room: o)
            lastEvent = "left · \(o)"
        }
        sendState()
    }

    // Foreground tick: RSSI grace-leave + a low-cadence keepalive. While the app
    // is open, if the current room hasn't been "near" for exitGrace, check out —
    // so walking away (or just being far) clears you even if still in the region.
    private func tick() {
        if let cur = currentRoom,
           Date().timeIntervalSince(lastSeenNear[cur] ?? .distantPast) > Self.exitGrace {
            setCurrentRoom(nil, by: "tick.grace")
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

    private var lastNotifyAt = Date.distantPast

    private func notify(entered: Bool, room: String) {
        guard AppSettings.notifyOnCheckInOut else { return }
        // Debounce: rapid flapping (e.g. two beacons in range at once) must not
        // spam a burst of notifications. Cap to one every 12s.
        let now = Date()
        guard now.timeIntervalSince(lastNotifyAt) > 12 else {
            EventLog.log("notify.suppressed", "entered=\(entered) room=\(room)")
            return
        }
        lastNotifyAt = now
        EventLog.log("notify.fire", "entered=\(entered) room=\(room)")
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

    // MARK: - diagnostics (on-screen, to debug background behaviour)
    private func bumpBgEnter() {
        let n = UserDefaults.standard.integer(forKey: "rp_bg_enters") + 1
        UserDefaults.standard.set(n, forKey: "rp_bg_enters")
        updateDiag()
    }

    func updateDiag() {
        // Location permission — "Always" is the bar for closed-app check-in.
        let authValue: String
        let authLevel: DiagLevel
        let authHint: String?
        switch manager.authorizationStatus {
        case .authorizedAlways:   authValue = "Always";      authLevel = .ok;   authHint = nil
        case .authorizedWhenInUse: authValue = "While Using"; authLevel = .warn; authHint = "Allow “Always” so check-in works with the app closed."
        case .denied:             authValue = "Denied";      authLevel = .bad;  authHint = "Turn on Location in Settings."
        case .restricted:         authValue = "Restricted";  authLevel = .bad;  authHint = "Location is restricted on this device."
        case .notDetermined:      authValue = "Not set";     authLevel = .bad;  authHint = "Tap “Enable auto check-in” to grant access."
        @unknown default:         authValue = "Unknown";     authLevel = .bad;  authHint = nil
        }

        let precise = manager.accuracyAuthorization == .fullAccuracy
        let regions = manager.monitoredRegions.count
        let bg = UserDefaults.standard.integer(forKey: "rp_bg_enters")
        let host = URL(string: AppSettings.backendBaseURL)?.host ?? AppSettings.backendBaseURL

        let rows: [DiagRow] = [
            DiagRow(id: "auth", label: "Location access", value: authValue,
                    level: authLevel, hint: authHint),
            DiagRow(id: "precise", label: "Precise location", value: precise ? "On" : "Reduced",
                    level: precise ? .ok : .bad,
                    hint: precise ? nil : "iBeacons need Precise location — turn it on in Settings."),
            DiagRow(id: "regions", label: "Rooms monitored", value: "\(regions)",
                    level: regions > 0 ? .ok : .bad,
                    hint: regions > 0 ? nil : "No rooms registered yet — check the backend connection."),
            DiagRow(id: "bg", label: "Background check-ins", value: "\(bg)",
                    level: .info,
                    hint: bg == 0 ? "Walk out of range and back with the app closed to confirm." : nil),
            DiagRow(id: "backend", label: "Backend", value: host, level: .info, hint: nil),
        ]

        let ready = manager.authorizationStatus == .authorizedAlways && precise && regions > 0
        let summary: String
        if ready {
            summary = "Ready for closed-app check-in"
        } else if authLevel == .bad {
            summary = "Auto check-in needs location access"
        } else if !precise {
            summary = "Turn on Precise location"
        } else if regions == 0 {
            summary = "Waiting for rooms to load"
        } else {
            summary = "Allow “Always” for closed-app check-in"
        }

        let line = "perm=\(authValue) · \(precise ? "Precise" : "Reduced") · regions=\(regions) · bgEnters=\(bg) · backend=\(host)"

        let apply = {
            self.diagRows = rows
            self.diagReady = ready
            self.diagSummary = summary
            self.diag = line
        }
        if Thread.isMainThread { apply() } else { DispatchQueue.main.async { apply() } }
    }

    /// Push the current diagnostics snapshot to the backend (for remote review).
    func sendDiagnostics(completion: @escaping (Bool) -> Void) {
        updateDiag() // capture the freshest state before sending
        let rows = diagRows.map { row -> [String: String] in
            ["label": row.label, "value": row.value, "level": row.level.name, "hint": row.hint ?? ""]
        }
        DiagClient.send(deviceID: AppSettings.deviceID,
                        displayName: AppSettings.userID,
                        summary: diagSummary,
                        ready: diagReady,
                        line: diag,
                        rows: rows,
                        completion: completion)
    }

    /// Push the full persistent event log to the backend (POST /history) so the
    /// background timeline (region callbacks, check-ins, notifications, lock/unlock)
    /// can be reviewed remotely.
    func sendHistory(completion: @escaping (Bool) -> Void) {
        EventLog.log("history.send", appCtx())
        HistoryClient.send(deviceID: AppSettings.deviceID,
                           name: AppSettings.userID,
                           events: EventLog.snapshot(),
                           completion: completion)
    }
}

extension RoomMonitor: CLLocationManagerDelegate {
    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        EventLog.log("cl.auth", "auth=\(manager.authorizationStatus.rawValue) accuracy=\(manager.accuracyAuthorization.rawValue)")
        updateDiag()
        switch manager.authorizationStatus {
        case .authorizedAlways:
            needsAlwaysInSettings = false
            manager.allowsBackgroundLocationUpdates = true
            if manager.monitoredRegions.isEmpty { startMonitoringRooms() }
            statusText = "Auto check-in on — even when the app is closed"
        case .authorizedWhenInUse:
            if manager.monitoredRegions.isEmpty { startMonitoringRooms() }
            if !askedAlways {
                askedAlways = true
                manager.requestAlwaysAuthorization() // shows the system "Always" dialog
                statusText = "Allow “Always” so check-in works with the app closed"
            } else {
                needsAlwaysInSettings = true
                statusText = "Working while open — allow “Always” in Settings for closed-app check-in"
            }
        case .denied, .restricted:
            needsAlwaysInSettings = true
            statusText = "Location denied — enable it in Settings"
            isMonitoring = false
        default:
            break
        }
    }

    /// FOREGROUND authority: while the app is open we have real signal strength,
    /// so ranging — not the coarse region — decides the room. You're "in" only if a
    /// beacon is at/above your sensitivity threshold; a far beacon you merely share
    /// a region with reads "too far" and the grace timer checks you out. (Background
    /// can't range cheaply, so it stays region-driven — tune that with TX/shielding.)
    func locationManager(_ manager: CLLocationManager,
                         didRange beacons: [CLBeacon],
                         satisfying constraint: CLBeaconIdentityConstraint) {
        let enterRSSI = AppSettings.rssiThreshold
        var bestRoom: String?
        var bestRSSI = Int.min
        var qualifying = 0
        var live: [RangedBeacon] = []
        for b in beacons {
            let preset = room(major: b.major.intValue, minor: b.minor.intValue)
            let name = preset?.name ?? "Room \(b.major)/\(b.minor)"
            live.append(RangedBeacon(id: "\(b.major).\(b.minor)", room: name,
                                     proximity: Self.proximityText(b.proximity),
                                     accuracy: b.accuracy, rssi: b.rssi))
            if let preset, b.rssi != 0, b.rssi >= enterRSSI {
                qualifying += 1
                if b.rssi > bestRSSI { bestRSSI = b.rssi; bestRoom = preset.name }
            }
        }
        liveBeacons = live.sorted { $0.room < $1.room }

        // Throttled summary so the history shows whether a real beacon was present.
        let now = Date()
        if now.timeIntervalSince(lastRangeLogAt) > 5 {
            lastRangeLogAt = now
            EventLog.log("range", "count=\(beacons.count) qual=\(qualifying) best=\(bestRoom.map { "\($0)@\(bestRSSI)" } ?? "-") thr=\(enterRSSI)")
        }

        if let r = bestRoom {
            lastSeenNear[r] = Date()
            nearHits[r, default: 0] += 1
            for k in Array(nearHits.keys) where k != r { nearHits[k] = 0 }
            if nearHits[r, default: 0] >= Self.enterThreshold {
                setCurrentRoom(r, by: "didRange")
            }
        } else {
            nearHits.removeAll()
            // Leaving is handled by the grace timer (tick), so one dropped sample
            // doesn't bounce you out.
        }
    }

    /// BACKGROUND authority: when the app is closed/backgrounded we can't range
    /// cheaply, so low-power region monitoring drives presence — in while inside the
    /// beacon's region, out when you leave it. Its boundary is the RF range (tune
    /// with TX power + shielding). When the app is OPEN, these are ignored and the
    /// ranging engine (didRange) owns the state, so it reflects real proximity.
    func locationManager(_ manager: CLLocationManager, didEnterRegion region: CLRegion) {
        EventLog.log("cl.enterRegion", "id=\(region.identifier) \(appCtx())")
        guard UIApplication.shared.applicationState != .active else { return }
        bumpBgEnter()
        guard let room = room(for: region) else { return }
        // A real boundary CROSSING (iOS freshly detected the beacon) — check in.
        setCurrentRoom(room.name, by: "enterRegion")
    }

    func locationManager(_ manager: CLLocationManager, didExitRegion region: CLRegion) {
        EventLog.log("cl.exitRegion", "id=\(region.identifier) \(appCtx())")
        guard UIApplication.shared.applicationState != .active else { return }
        guard let room = room(for: region) else { return }
        if currentRoom == room.name { setCurrentRoom(nil, by: "exitRegion") }
    }

    func locationManager(_ manager: CLLocationManager, didDetermineState state: CLRegionState, for region: CLRegion) {
        EventLog.log("cl.determineState", "state=\(state.rawValue) id=\(region.identifier) \(appCtx())")
        guard UIApplication.shared.applicationState != .active else { return } // foreground: ranging owns state
        guard let room = room(for: region) else { return }
        switch state {
        case .inside:
            // DO NOT check in here. didDetermineState replays CACHED/stale region
            // state — on relaunch, on screen-wake, and on every re-startMonitoring —
            // and iOS holds "inside" for minutes after a beacon is physically gone.
            // That is exactly what produced phantom check-ins with no beacon nearby
            // (confirmed root cause). Real arrival is handled by didEnterRegion.
            bumpBgEnter()
        case .outside:
            if currentRoom == room.name { setCurrentRoom(nil, by: "determineState.outside") }
        default:
            break
        }
    }

    func locationManager(_ manager: CLLocationManager, monitoringDidFailFor region: CLRegion?, withError error: Error) {
        EventLog.log("cl.monitorFail", "id=\(region?.identifier ?? "?") err=\(error.localizedDescription)")
    }
    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        EventLog.log("cl.fail", "err=\(error.localizedDescription)")
    }
}
