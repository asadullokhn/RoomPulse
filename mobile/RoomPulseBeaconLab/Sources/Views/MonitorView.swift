import SwiftUI

/// The main screen: set your name, enable once, forget the app. Production-clean —
/// tuning and diagnostics live under a collapsed Advanced section.
struct MonitorView: View {
    @StateObject private var monitor = RoomMonitor.shared
    @State private var userID = AppSettings.userID
    @State private var backendURL = AppSettings.backendBaseURL
    @State private var rssi = AppSettings.rssiThreshold
    @State private var notify = AppSettings.notifyOnCheckInOut
    @State private var showAdvanced = false

    private var currentRoom: String? { monitor.insideRooms.sorted().first }

    private var headline: String {
        guard monitor.isMonitoring else { return "Auto check-in is off" }
        return currentRoom.map { "You’re in \($0)" } ?? "Not in a room"
    }
    private var heroIcon: String {
        guard monitor.isMonitoring else { return "moon.zzz.fill" }
        return currentRoom != nil ? "checkmark.circle.fill" : "dot.radiowaves.left.and.right"
    }
    private var heroColor: Color {
        guard monitor.isMonitoring else { return .gray }
        return currentRoom != nil ? .green : .accentColor
    }

    var body: some View {
        NavigationStack {
            Form {
                // --- Status + primary control ---
                Section {
                    HStack(spacing: 13) {
                        Image(systemName: heroIcon)
                            .font(.system(size: 28))
                            .foregroundStyle(heroColor)
                            .frame(width: 34)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(headline).font(.headline)
                            Text(monitor.statusText).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                    }
                    .padding(.vertical, 4)

                    if monitor.isMonitoring {
                        Button(role: .destructive) { monitor.disable() } label: {
                            Label("Turn off auto check-in", systemImage: "stop.fill")
                        }
                    } else {
                        Button { monitor.enableBackgroundCheckIn() } label: {
                            Label("Enable auto check-in", systemImage: "sparkles")
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                    }
                    if monitor.needsAlwaysInSettings {
                        Button { monitor.openSettings() } label: {
                            Label("Allow “Always” in Settings", systemImage: "gear")
                        }
                    }
                    if !monitor.lastEvent.isEmpty {
                        LabeledContent("Last event", value: monitor.lastEvent).font(.caption)
                    }
                } footer: {
                    Text("Enable once, then forget the app. RoomPulse checks you in when you’re near a room and out when you leave — even when it’s closed.")
                }

                // --- Identity ---
                Section("You") {
                    TextField("Your name", text: $userID)
                        .textInputAutocapitalization(.words)
                        .onChange(of: userID) { newValue in AppSettings.userID = newValue }
                }

                // --- Notifications ---
                Section {
                    Toggle("Check-in notifications", isOn: $notify)
                        .onChange(of: notify) { on in
                            AppSettings.notifyOnCheckInOut = on
                            if on { RoomMonitor.shared.requestNotificationAuthorization() }
                        }
                } header: {
                    Text("Notifications")
                } footer: {
                    Text("Off by default — check-in is silent. Turn on to get a notification when you arrive or leave.")
                }

                // --- Advanced (collapsed) ---
                Section {
                    DisclosureGroup("Advanced", isExpanded: $showAdvanced) {
                        Stepper(value: $rssi, in: -90 ... -40, step: 1) {
                            Text("Sensitivity: \(rssi) dBm")
                        }
                        .onChange(of: rssi) { newValue in AppSettings.rssiThreshold = newValue }
                        Text("Higher (e.g. −55) means you must be closer to check in. Watch the live signal below to tune it.")
                            .font(.caption2).foregroundStyle(.secondary)

                        if monitor.liveBeacons.isEmpty {
                            LabeledContent("Live signal", value: "no beacons in range")
                                .foregroundStyle(.secondary)
                        } else {
                            ForEach(monitor.liveBeacons) { b in
                                let valid = b.rssi != 0
                                let near = valid && b.rssi >= rssi
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(b.room).font(.subheadline)
                                        Text(!valid ? "no reading" : (near ? "in range" : "too far"))
                                            .font(.caption2).foregroundStyle(near ? .green : .secondary)
                                    }
                                    Spacer()
                                    Text(valid ? "\(b.rssi) dBm" : "—")
                                        .font(.callout.bold().monospacedDigit())
                                        .foregroundStyle(near ? .green : .secondary)
                                }
                            }
                        }

                        TextField("Backend URL", text: $backendURL)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.URL)
                            .onChange(of: backendURL) { newValue in AppSettings.backendBaseURL = newValue }
                        LabeledContent("Device ID", value: AppSettings.deviceID)
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            .navigationTitle("RoomPulse")
        }
    }
}
