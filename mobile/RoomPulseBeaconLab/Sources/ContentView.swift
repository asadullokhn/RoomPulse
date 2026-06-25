import SwiftUI

struct ContentView: View {
    var body: some View {
        TabView {
            MonitorView()
                .tabItem { Label("RoomPulse", systemImage: "sparkles") }
            TransmitView()
                .tabItem { Label("Beacon", systemImage: "dot.radiowaves.left.and.right") }
        }
    }
}
