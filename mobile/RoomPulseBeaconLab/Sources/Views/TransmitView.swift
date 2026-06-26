import SwiftUI

/// Turns this phone into a room beacon for bench testing.
struct TransmitView: View {
    @StateObject private var tx = BeaconTransmitter()
    @ObservedObject private var registry = RoomRegistry.shared
    @State private var selected: RoomPreset = RoomRegistry.shared.rooms.first ?? RoomPreset.defaults[0]

    var body: some View {
        NavigationStack {
            Form {
                Section("Pretend this phone is a room beacon") {
                    Picker("Room", selection: $selected) {
                        ForEach(registry.rooms) { room in
                            Text(room.name).tag(room)
                        }
                    }
                    LabeledContent("Major / Minor", value: "\(selected.major) / \(selected.minor)")
                    LabeledContent("Workspace", value: selected.workspaceID)
                }

                Section {
                    if tx.isAdvertising {
                        Button(role: .destructive) { tx.stop() } label: {
                            Label("Stop broadcasting", systemImage: "stop.fill")
                        }
                    } else {
                        Button { tx.start(room: selected) } label: {
                            Label("Start broadcasting", systemImage: "dot.radiowaves.left.and.right")
                        }
                    }
                    LabeledContent("Status", value: tx.statusText)
                }

                Section {
                    Text(BeaconConstants.uuid.uuidString)
                        .font(.footnote.monospaced())
                        .foregroundStyle(.secondary)
                    Text("Keep this screen open and the phone unlocked — iOS stops the beacon when the app is backgrounded.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .navigationTitle("Transmit")
        }
    }
}
