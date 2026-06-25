import Foundation
import CoreBluetooth
import CoreLocation

/// Advertises this iPhone as an iBeacon. NOTE: iOS only broadcasts while the app
/// is in the foreground and the screen is awake — backgrounding stops it. That's
/// why a real deployment needs the ESP32; this is for bench testing.
final class BeaconTransmitter: NSObject, ObservableObject {
    @Published var isAdvertising = false
    @Published var statusText = "Idle"

    private var manager: CBPeripheralManager!
    private var pendingData: [String: Any]?

    override init() {
        super.init()
        manager = CBPeripheralManager(delegate: self, queue: nil)
    }

    func start(room: RoomPreset) {
        let region = CLBeaconRegion(
            uuid: BeaconConstants.uuid,
            major: room.major,
            minor: room.minor,
            identifier: "RoomPulse-\(room.name)"
        )
        let data = (region.peripheralData(withMeasuredPower: nil) as? [String: Any]) ?? [:]
        pendingData = data
        if manager.state == .poweredOn {
            manager.startAdvertising(data)
        } else {
            statusText = "Waiting for Bluetooth to power on…"
        }
    }

    func stop() {
        manager.stopAdvertising()
        isAdvertising = false
        statusText = "Stopped"
    }
}

extension BeaconTransmitter: CBPeripheralManagerDelegate {
    func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        DispatchQueue.main.async {
            switch peripheral.state {
            case .poweredOn:
                if let data = self.pendingData {
                    peripheral.startAdvertising(data)
                } else {
                    self.statusText = "Bluetooth ready"
                }
            case .poweredOff:
                self.statusText = "Turn Bluetooth on"
            case .unauthorized:
                self.statusText = "Bluetooth permission denied"
            case .unsupported:
                self.statusText = "BLE not supported on this device"
            default:
                self.statusText = "Bluetooth unavailable"
            }
        }
    }

    func peripheralManagerDidStartAdvertising(_ peripheral: CBPeripheralManager, error: Error?) {
        DispatchQueue.main.async {
            if let error = error {
                self.statusText = "Error: \(error.localizedDescription)"
                self.isAdvertising = false
            } else {
                self.statusText = "Broadcasting"
                self.isAdvertising = true
            }
        }
    }
}
