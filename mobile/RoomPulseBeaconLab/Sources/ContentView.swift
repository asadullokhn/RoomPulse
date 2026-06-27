import SwiftUI

struct ContentView: View {
    var body: some View {
        TabView {
            MonitorView()
                .tabItem { Label("RoomPulse", systemImage: "dot.radiowaves.left.and.right") }
            TransmitView()
                .tabItem { Label("Beacon", systemImage: "antenna.radiowaves.left.and.right") }
        }
        .tint(Brand.teal)
    }
}
