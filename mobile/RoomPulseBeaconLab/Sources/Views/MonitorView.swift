import SwiftUI

/// The single main page: set your name, enable once, forget the app. Shows live
/// signal, who's inside, and status — Auto + Detect merged into one.
struct MonitorView: View {
    @StateObject private var monitor = RoomMonitor.shared
    @State private var userID = AppSettings.userID
    @State private var backendURL = AppSettings.backendBaseURL
    @State private var rssi = AppSettings.rssiThreshold
    @State private var notify = AppSettings.notifyOnCheckInOut
    @State private var showAdvanced = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("Set your name, enable once, then forget the app. RoomPulse checks you in when you're near a room and out when you leave — even with the app closed.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }

                Section("You") {
                    TextField("Your name", text: $userID)
                        .textInputAutocapitalization(.words)
                        .onChange(of: userID) { newValue in AppSettings.userID = newValue }
                }

                Section {
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
                    LabeledContent("Status", value: monitor.statusText)
                    if !monitor.lastEvent.isEmpty {
                        LabeledContent("Last event", value: monitor.lastEvent)
                    }
                    if monitor.needsAlwaysInSettings {
                        Button { monitor.openSettings() } label: {
                            Label("Allow “Always” in Settings", systemImage: "gear")
                        }
                    }
                }

                Section("Inside now") {
                    if monitor.insideRooms.isEmpty {
                        Text("Not in any room").foregroundStyle(.secondary)
                    } else {
                        ForEach(monitor.insideRooms.sorted(), id: \.self) { room in
                            Label(room, systemImage: "checkmark.circle.fill")
                                .foregroundStyle(.green)
                        }
                    }
                }

                Section("Check-in sensitivity") {
                    Stepper(value: $rssi, in: -90 ... -40, step: 1) {
                        Text("Threshold: \(rssi) dBm")
                    }
                    .onChange(of: rssi) { newValue in AppSettings.rssiThreshold = newValue }
                    Text("Stand in the room and read your RSSI below, then step out and read it again. Set the threshold between the two. Higher (e.g. −55) = must be closer.")
                        .font(.caption2).foregroundStyle(.secondary)
                }

                Section("Notifications") {
                    Toggle("Notify on check-in / out", isOn: $notify)
                        .onChange(of: notify) { on in
                            AppSettings.notifyOnCheckInOut = on
                            if on { RoomMonitor.shared.requestNotificationAuthorization() }
                        }
                    Text("Off by default. RoomPulse checks you in silently — turn this on to get a notification each time you arrive or leave.")
                        .font(.caption2).foregroundStyle(.secondary)
                }

                Section("Live signal") {
                    if monitor.liveBeacons.isEmpty {
                        Text("No beacons in range").foregroundStyle(.secondary)
                    } else {
                        ForEach(monitor.liveBeacons) { b in
                            let valid = b.rssi != 0
                            let near = valid && b.rssi >= rssi
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(b.room).font(.subheadline)
                                    Text(!valid ? "no reading" : (near ? "in range — checks in" : "too far"))
                                        .font(.caption2)
                                        .foregroundStyle(near ? .green : .secondary)
                                }
                                Spacer()
                                Text(valid ? "\(b.rssi) dBm" : "—")
                                    .font(.callout.bold().monospacedDigit())
                                    .foregroundStyle(near ? .green : .secondary)
                            }
                        }
                    }
                    Text("Live signal updates only while this screen is open. Background check-in keeps working when closed.")
                        .font(.caption2).foregroundStyle(.secondary)
                }

                Section {
                    DisclosureGroup("Advanced", isExpanded: $showAdvanced) {
                        TextField("Backend URL", text: $backendURL)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.URL)
                            .onChange(of: backendURL) { newValue in AppSettings.backendBaseURL = newValue }
                    }
                }
            }
            .navigationTitle("RoomPulse")
        }
    }
}
