# App TX Tuner Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A third "TX Tuner" tab in RoomPulseBeaconLab that changes the nRF52840 beacon's TX power via the Mac's `txtuner.py` HTTP bridge, with a verified progress bar and live RSSI on the same screen.

**Architecture:** `TunerClient` (static enum, URLSession, mirrors `DiagClient`) calls the bridge's `/api/state` and `/api/tx`; `TunerView` renders state + live `RoomMonitor.shared.liveBeacons` RSSI + the 14-level button set and animates a progress bar that completes only on the bridge's verified response. One new persisted setting for the bridge URL.

**Tech Stack:** SwiftUI (iOS 16), URLSession, xcodegen + xcodebuild (full Xcode), `xcrun devicectl` for device install.

**Spec:** `docs/superpowers/specs/2026-07-04-app-tx-tuner-design.md`

## Global Constraints

- Do NOT touch `Views/MonitorView.swift`, `Beacon/RoomMonitor.swift`, `Beacon/BeaconConstants.swift`, or the untracked `EventLog/DiagClient/HistoryClient` files — they belong to the in-flight diagnostics workstream. `git add` only the files this plan creates/modifies, never `git add -A`.
- TX level set (must equal firmware + txtuner.py): `-40 -20 -16 -12 -8 -4 0 2 3 4 5 6 7 8`; labeled five: `+8 max range`, `0 tag default`, `−12 C6 floor`, `−16 room start`, `−20 room tight`.
- Default bridge URL: `http://Asadullokhs-MacBook-Pro.local:8880`; request timeout 8 s.
- ATS/local-network plist keys already exist in `project.yml` (`NSAllowsLocalNetworking`, `NSLocalNetworkUsageDescription`) — no plist work.
- Build with full Xcode: `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer`, team `Y7ZZ5G2T3Y`; regenerate the project with `xcodegen generate` (xcodegen auto-includes new files under `Sources/`).
- The app has no unit-test target — each task's test cycle is "xcodebuild compiles" plus the on-device end-to-end check in Task 3.
- Auto-commit allowed (project memory `feedback_auto_commit_deploy`); never add `Co-Authored-By`.

---

### Task 1: TunerClient + tunerBaseURL setting

**Files:**
- Create: `mobile/RoomPulseBeaconLab/Sources/Net/TunerClient.swift`
- Modify: `mobile/RoomPulseBeaconLab/Sources/Settings/AppSettings.swift` (add one property at the end of the enum)

**Interfaces:**
- Consumes: `AppSettings` UserDefaults pattern (private static `d`).
- Produces: `TunerState { major, minor, tx, adv: Int; uuid: String? }`; `TunerError: LocalizedError`; `TunerClient.levels: [Int]`; `TunerClient.fetchState(completion:)` and `TunerClient.setTx(level:completion:)`, both completing on the main queue with `Result<TunerState, TunerError>`; `AppSettings.tunerBaseURL: String`. Task 2 consumes all of these.

- [ ] **Step 1: Create `TunerClient.swift`**

```swift
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
```

- [ ] **Step 2: Add the setting**

Append inside the `AppSettings` enum (after `notifyOnCheckInOut`):

```swift
    /// Base URL of the Mac-side TX tuner bridge (device/tools/txtuner.py).
    static var tunerBaseURL: String {
        get { d.string(forKey: "tunerBaseURL") ?? "http://Asadullokhs-MacBook-Pro.local:8880" }
        set { d.set(newValue, forKey: "tunerBaseURL") }
    }
```

- [ ] **Step 3: Verify it compiles**

```bash
cd mobile/RoomPulseBeaconLab && xcodegen generate
DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer xcodebuild \
  -project RoomPulseBeaconLab.xcodeproj -scheme RoomPulseBeaconLab \
  -destination 'generic/platform=iOS' -derivedDataPath build_txtuner \
  DEVELOPMENT_TEAM=Y7ZZ5G2T3Y -allowProvisioningUpdates build 2>&1 | tail -3
```

Expected: `** BUILD SUCCEEDED **`

- [ ] **Step 4: Commit**

```bash
git add mobile/RoomPulseBeaconLab/Sources/Net/TunerClient.swift \
        mobile/RoomPulseBeaconLab/Sources/Settings/AppSettings.swift
git commit -m "App: TunerClient for the Mac TX tuner bridge + tunerBaseURL setting"
```

---

### Task 2: TunerView + third tab

**Files:**
- Create: `mobile/RoomPulseBeaconLab/Sources/Views/TunerView.swift`
- Modify: `mobile/RoomPulseBeaconLab/Sources/ContentView.swift`

**Interfaces:**
- Consumes: `TunerClient.fetchState/setTx/levels`, `TunerState`, `AppSettings.tunerBaseURL` (Task 1); `RoomMonitor.shared.liveBeacons: [RangedBeacon]` (fields `room: String`, `rssi: Int`); `Brand.teal`.
- Produces: `TunerView` used by `ContentView`.

- [ ] **Step 1: Create `TunerView.swift`**

```swift
import SwiftUI

/// Live TX-power tuning for the nRF52840 lab beacon via the Mac's txtuner.py
/// bridge. The progress bar completes only after the bridge confirms the
/// firmware ack and re-reads the state, so "applied" means verified-applied.
struct TunerView: View {
    @StateObject private var monitor = RoomMonitor.shared
    @State private var state: TunerState?
    @State private var busy = false
    @State private var progress = 0.0
    @State private var bannerText = ""
    @State private var bannerOK = false
    @State private var serverURL = AppSettings.tunerBaseURL

    private static let labels: [Int: String] = [
        8: "max range", 0: "tag default", -12: "C6 floor",
        -16: "room start", -20: "room tight",
    ]
    private static let primary = [8, 0, -12, -16, -20]
    private static let secondary = [7, 6, 5, 4, 3, 2, -4, -8, -40]

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack(alignment: .firstTextBaseline) {
                        Text(state.map { Self.fmt($0.tx) } ?? "–")
                            .font(.system(size: 44, weight: .bold))
                        Text("dBm").foregroundStyle(.secondary)
                        Spacer()
                        Button { refresh() } label: { Image(systemName: "arrow.clockwise") }
                            .disabled(busy)
                    }
                    if let s = state {
                        Text("minor \(s.minor) · adv \(s.adv) ms · major \(s.major)")
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                    if busy {
                        ProgressView(value: progress)
                            .progressViewStyle(.linear)
                            .tint(Brand.teal)
                    }
                    if !bannerText.isEmpty {
                        Text(bannerText)
                            .font(.footnote)
                            .foregroundStyle(bannerOK ? .green : .red)
                    }
                }

                if !monitor.liveBeacons.isEmpty {
                    Section("Live signal") {
                        ForEach(monitor.liveBeacons) { b in
                            HStack {
                                Text(b.room)
                                Spacer()
                                Text(b.rssi != 0 ? "\(b.rssi) dBm" : "–")
                                    .monospacedDigit().foregroundStyle(.secondary)
                            }
                        }
                    }
                }

                Section("TX power") {
                    ForEach(Self.primary, id: \.self) { level in
                        levelButton(level, label: Self.labels[level])
                    }
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 3), spacing: 8) {
                        ForEach(Self.secondary, id: \.self) { level in
                            levelButton(level, label: nil)
                        }
                    }
                }

                Section("Tuner server (Mac)") {
                    TextField("http://mac.local:8880", text: $serverURL)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onSubmit {
                            AppSettings.tunerBaseURL = serverURL
                            refresh()
                        }
                }
            }
            .navigationTitle("TX Tuner")
            .onAppear { refresh() }
        }
    }

    private static func fmt(_ n: Int) -> String { n > 0 ? "+\(n)" : "\(n)" }

    @ViewBuilder
    private func levelButton(_ level: Int, label: String?) -> some View {
        Button {
            apply(level)
        } label: {
            VStack(spacing: 2) {
                Text("\(Self.fmt(level)) dBm").fontWeight(.semibold)
                if let label {
                    Text(label).font(.caption2).foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .tint(state?.tx == level ? Brand.teal : .gray)
        .disabled(busy)
    }

    private func refresh() {
        TunerClient.fetchState { result in
            switch result {
            case .success(let s):
                state = s
                bannerText = ""
            case .failure(let e):
                bannerText = e.localizedDescription
                bannerOK = false
            }
        }
    }

    private func apply(_ level: Int) {
        busy = true
        bannerText = ""
        progress = 0
        withAnimation(.easeOut(duration: 1.4)) { progress = 0.88 }
        TunerClient.setTx(level: level) { result in
            switch result {
            case .success(let s):
                withAnimation(.easeIn(duration: 0.2)) { progress = 1.0 }
                state = s
                bannerText = "Applied \(Self.fmt(level)) dBm"
                bannerOK = true
            case .failure(let e):
                progress = 0
                bannerText = e.localizedDescription
                bannerOK = false
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.7) {
                busy = false
                progress = 0
            }
        }
    }
}
```

- [ ] **Step 2: Add the tab**

In `ContentView.swift`, after the `TransmitView()` tab item:

```swift
            TunerView()
                .tabItem { Label("TX Tuner", systemImage: "slider.horizontal.3") }
```

- [ ] **Step 3: Verify it compiles**

```bash
cd mobile/RoomPulseBeaconLab
DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer xcodebuild \
  -project RoomPulseBeaconLab.xcodeproj -scheme RoomPulseBeaconLab \
  -destination 'generic/platform=iOS' -derivedDataPath build_txtuner \
  DEVELOPMENT_TEAM=Y7ZZ5G2T3Y -allowProvisioningUpdates build 2>&1 | tail -3
```

Expected: `** BUILD SUCCEEDED **`

- [ ] **Step 4: Commit**

```bash
git add mobile/RoomPulseBeaconLab/Sources/Views/TunerView.swift \
        mobile/RoomPulseBeaconLab/Sources/ContentView.swift
git commit -m "App: TX Tuner tab with live RSSI and verified-progress apply"
```

---

### Task 3: Install on device + end-to-end verification

**Files:** none (deploy + verify)

**Interfaces:**
- Consumes: the built `.app` from Task 2; running `txtuner.py` on the Mac; beacon on USB.

- [ ] **Step 1: Ensure the bridge is running**

```bash
curl -s -m 2 localhost:8880/api/state || python3 device/tools/txtuner.py &
```

Expected: state JSON (bridge already runs in this session's background).

- [ ] **Step 2: Install and launch on the iPhone (must be unlocked)**

```bash
xcrun devicectl list devices
xcrun devicectl device install app --device <DEVICE_ID> \
  mobile/RoomPulseBeaconLab/build_txtuner/Build/Products/Debug-iphoneos/RoomPulseBeaconLab.app
xcrun devicectl device process launch --device <DEVICE_ID> net.roompulse.beaconlab
```

Expected: install + launch succeed. (`<DEVICE_ID>` from the list output.)

- [ ] **Step 3: On-device check (user, ~1 minute)**

In the app's **TX Tuner** tab: state card shows the beacon's current TX; approve the Local Network prompt on first request if iOS shows it. Tap `−16 room start` → bar completes → "Applied −16 dBm". Cross-check from the Mac:

```bash
swift device/tools/blescan.swift 2>&1 | grep -E "4C000215111111112222"
```

Expected: RSSI noticeably lower than at `+8` (~15–25 dB at the same spot). Tap `+8 max range` to restore.

- [ ] **Step 4: Record in the work log**

Append a row to the 2026-07-04 section of the Obsidian `Challenge Work Log.md`: TX tuner (Mac bridge + web page + app tab) shipped; note the `cat`→`bat` stray-reader root cause found during the build.
