import SwiftUI
import UIKit

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
        return currentRoom != nil ? .green : Brand.teal
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
                                .foregroundStyle(Brand.teal)
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
                        DiagnosticsCard(monitor: monitor)
                    }
                }
            }
            .navigationTitle("RoomPulse")
            .tint(Brand.teal)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeOut(duration: 2.2).repeatForever(autoreverses: false)) { pulse = true }
            }
        }
    }
}

/// Health panel for the (Advanced) section: a one-line verdict plus a colour-coded
/// checklist of the things closed-app check-in actually depends on. Replaces the
/// old single monospaced debug string with something a non-engineer can read.
private struct DiagnosticsCard: View {
    @ObservedObject var monitor: RoomMonitor
    @State private var copied = false
    @State private var sending = false
    @State private var sendResult: Bool? = nil   // nil = idle, true/false = last outcome
    @State private var sendingHist = false
    @State private var histResult: Bool? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Verdict
            HStack(spacing: 8) {
                Image(systemName: monitor.diagReady ? "checkmark.seal.fill" : "exclamationmark.triangle.fill")
                    .foregroundStyle(monitor.diagReady ? .green : .orange)
                Text(monitor.diagSummary)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(monitor.diagReady ? .green : .primary)
                Spacer()
            }

            // Checklist
            ForEach(monitor.diagRows) { row in
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 10) {
                        Image(systemName: row.level.icon)
                            .font(.footnote)
                            .foregroundStyle(row.level.color)
                            .frame(width: 18)
                        Text(row.label).font(.subheadline)
                        Spacer(minLength: 8)
                        Text(row.value)
                            .font(.subheadline.weight(.medium).monospacedDigit())
                            .foregroundStyle(row.level == .info ? .secondary : row.level.color)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                    if let hint = row.hint {
                        Text(hint)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.leading, 28)
                    }
                }
            }

            // Actions: copy the one-line readout, or push the full snapshot to
            // the backend so it can be reviewed remotely (GET /diag).
            HStack(spacing: 18) {
                Button {
                    UIPasteboard.general.string = monitor.diag
                    copied = true
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { copied = false }
                } label: {
                    Label(copied ? "Copied" : "Copy",
                          systemImage: copied ? "checkmark" : "doc.on.doc")
                }
                .buttonStyle(.borderless)

                Button {
                    sending = true
                    sendResult = nil
                    monitor.sendDiagnostics { ok in
                        sending = false
                        sendResult = ok
                        DispatchQueue.main.asyncAfter(deadline: .now() + 2.5) { sendResult = nil }
                    }
                } label: {
                    HStack(spacing: 6) {
                        if sending {
                            ProgressView().controlSize(.small)
                        } else {
                            Image(systemName: sendResult == nil ? "icloud.and.arrow.up"
                                : (sendResult == true ? "checkmark.icloud.fill" : "exclamationmark.icloud"))
                        }
                        Text(sending ? "Sending…"
                            : (sendResult == nil ? "Send to backend"
                            : (sendResult == true ? "Sent" : "Failed")))
                    }
                    .foregroundStyle(sendResult == false ? .red : Brand.teal)
                }
                .buttonStyle(.borderless)
                .disabled(sending)

                Spacer()
            }
            .font(.caption)
            .tint(Brand.teal)

            // Full event history — tap this when something misbehaves, so the
            // whole background timeline (region events, check-ins, lock/unlock)
            // lands on the backend for review.
            Button {
                sendingHist = true
                histResult = nil
                monitor.sendHistory { ok in
                    sendingHist = false
                    histResult = ok
                    DispatchQueue.main.asyncAfter(deadline: .now() + 2.5) { histResult = nil }
                }
            } label: {
                HStack(spacing: 6) {
                    if sendingHist {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: histResult == nil ? "clock.arrow.circlepath"
                            : (histResult == true ? "checkmark.icloud.fill" : "exclamationmark.icloud"))
                    }
                    Text(sendingHist ? "Sending…"
                        : (histResult == nil ? "Send full history"
                        : (histResult == true ? "History sent" : "Failed")))
                }
                .font(.caption)
                .foregroundStyle(histResult == false ? .red : Brand.teal)
            }
            .buttonStyle(.borderless)
            .disabled(sendingHist)
        }
        .padding(.vertical, 4)
        .onAppear { monitor.updateDiag() }
    }
}

private extension DiagLevel {
    var color: Color {
        switch self {
        case .ok:   return .green
        case .warn: return .orange
        case .bad:  return .red
        case .info: return Brand.teal
        }
    }
    var icon: String {
        switch self {
        case .ok:   return "checkmark.circle.fill"
        case .warn: return "exclamationmark.triangle.fill"
        case .bad:  return "xmark.circle.fill"
        case .info: return "info.circle.fill"
        }
    }
}
