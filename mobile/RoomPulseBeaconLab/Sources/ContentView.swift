import SwiftUI

struct ContentView: View {
    var body: some View {
        TabView {
            MonitorView()
                .tabItem { Label("RoomPulse", systemImage: "dot.radiowaves.left.and.right") }
            TransmitView()
                .tabItem { Label("Beacon", systemImage: "antenna.radiowaves.left.and.right") }
            TunerView()
                .tabItem { Label("TX Tuner", systemImage: "slider.horizontal.3") }
        }
        .tint(Brand.teal)
    }
}
