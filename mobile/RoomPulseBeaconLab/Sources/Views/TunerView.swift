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
