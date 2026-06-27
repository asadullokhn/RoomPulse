import SwiftUI

/// The main screen. Auto check-in is the whole point, so it's the hero: one
/// status, one action. Everything tunable or temporary (name, sensitivity,
/// signal, backend, device id) lives under a collapsed Advanced section.
struct MonitorView: View {
    @StateObject private var monitor = RoomMonitor.shared
    @State private var userID = AppSettings.userID
    @State private var backendURL = AppSettings.backendBaseURL
    @State private var rssi = AppSettings.rssiThreshold
    @State private var notify = AppSettings.notifyOnCheckInOut
    @State private var showAdvanced = false
    @State private var pulse = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private static let brand = Color(red: 0.0, green: 0.60, blue: 0.46)

    private var currentRoom: String? { monitor.insideRooms.sorted().first }
    private var scanning: Bool { monitor.isMonitoring && currentRoom == nil }

    private var headline: String {
        guard monitor.isMonitoring else { return "Auto check-in is off" }
        return currentRoom.map { "You’re in \($0)" } ?? "Looking for rooms…"
    }
    private var heroIcon: String {
        guard monitor.isMonitoring else { return "moon.zzz.fill" }
        return currentRoom != nil ? "checkmark.circle.fill" : "dot.radiowaves.left.and.right"
    }
    private var heroTint: Color {
        guard monitor.isMonitoring else { return .gray }
        return currentRoom != nil ? .green : Self.brand
    }

    var body: some View {
        NavigationStack {
            Form {
                // --- Hero: auto check-in status + the one action ---
                Section {
                    VStack(spacing: 16) {
                        ZStack {
                            if scanning && !reduceMotion {
                                Circle().stroke(heroTint.opacity(0.5), lineWidth: 2)
                                    .frame(width: 86, height: 86)
                                    .scaleEffect(pulse ? 1.5 : 0.85)
                                    .opacity(pulse ? 0 : 0.7)
                            }
                            Circle().fill(heroTint.opacity(0.14)).frame(width: 86, height: 86)
                            Image(systemName: heroIcon)
                                .font(.system(size: 34, weight: .medium))
                                .foregroundStyle(heroTint)
                        }
                        VStack(spacing: 4) {
                            Text(headline).font(.title2.weight(.semibold)).multilineTextAlignment(.center)
                            Text(monitor.statusText).font(.subheadline).foregroundStyle(.secondary)
                                .multilineTextAlignment(.center)
                        }

                        if !monitor.isMonitoring {
                            // The only thing to do: grant permission once.
                            Button { monitor.enableBackgroundCheckIn() } label: {
                                Text("Enable auto check-in").font(.headline).frame(maxWidth: .infinity)
                            }
                            .buttonStyle(.borderedProminent).controlSize(.large)
                        } else if !monitor.needsAlwaysInSettings {
                            // Working — just confirm it, no off switch.
                            Label("On — running in the background", systemImage: "checkmark.circle.fill")
                                .font(.subheadline.weight(.medium))
                                .foregroundStyle(Self.brand)
                        }

                        if monitor.needsAlwaysInSettings {
                            Button { monitor.openSettings() } label: {
                                Label("Allow “Always” in Settings", systemImage: "gear").font(.footnote)
                            }
                        }
                        if !monitor.lastEvent.isEmpty {
                            Text(monitor.lastEvent).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 12)
                    .listRowBackground(Color.clear)
                } footer: {
                    Text("Enable once and forget it. RoomPulse checks you in when you’re near a room and out when you leave — even when it’s closed.")
                        .frame(maxWidth: .infinity)
                        .multilineTextAlignment(.center)
                }

                // --- Notifications (a real user setting) ---
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
                        VStack(alignment: .leading, spacing: 4) {
                            TextField("Your name", text: $userID)
                                .textInputAutocapitalization(.words)
                                .onChange(of: userID) { newValue in AppSettings.userID = newValue }
                            Text("Temporary — replaced by sign-in later.")
                                .font(.caption2).foregroundStyle(.secondary)
                        }

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
            .tint(Self.brand)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeOut(duration: 2.2).repeatForever(autoreverses: false)) { pulse = true }
            }
        }
    }
}
