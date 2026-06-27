import SwiftUI

/// Turns this phone into a room beacon for bench testing.
struct TransmitView: View {
    @StateObject private var tx = BeaconTransmitter()
    @ObservedObject private var registry = RoomRegistry.shared
    @State private var selected: RoomPreset = RoomRegistry.shared.rooms.first ?? RoomPreset.defaults[0]
    @State private var showAdvanced = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Room", selection: $selected) {
                        ForEach(registry.rooms) { room in
                            Text(room.name).tag(room)
                        }
                    }
                } header: {
                    Text("Broadcast as a room")
                } footer: {
                    Text("Bench testing — turns this phone into a room beacon other phones can detect.")
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
                } footer: {
                    Text("Keep this screen open and the phone unlocked — iOS stops the beacon when the app is backgrounded.")
                }

                Section {
                    DisclosureGroup("Advanced", isExpanded: $showAdvanced) {
                        LabeledContent("Major / Minor", value: "\(selected.major) / \(selected.minor)")
                        LabeledContent("Workspace", value: selected.workspaceID)
                        VStack(alignment: .leading, spacing: 4) {
                            Text("Beacon UUID").font(.caption).foregroundStyle(.secondary)
                            Text(BeaconConstants.uuid.uuidString).font(.footnote.monospaced())
                        }
                    }
                }
            }
            .navigationTitle("Transmit")
            .onAppear { RoomRegistry.shared.refresh() } // pull the latest rooms to broadcast as
            .onChange(of: registry.rooms) { rooms in
                if !rooms.contains(selected) { selected = rooms.first ?? selected }
            }
        }
    }
}
