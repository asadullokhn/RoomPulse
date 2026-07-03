import Foundation
import CoreBluetooth

final class Scanner: NSObject, CBCentralManagerDelegate {
    var central: CBCentralManager!
    var total = 0
    var withMfg = 0
    var ibeacons = 0
    var seen = Set<UUID>()
    override init() { super.init(); central = CBCentralManager(delegate: self, queue: nil) }

    func centralManagerDidUpdateState(_ c: CBCentralManager) {
        switch c.state {
        case .poweredOn:
            print("BT poweredOn — scanning 10s (all devices)...")
            c.scanForPeripherals(withServices: nil,
                                 options: [CBCentralManagerScanOptionAllowDuplicatesKey: false])
        case .poweredOff:    print("RESULT: Mac Bluetooth is OFF"); exit(2)
        case .unauthorized:  print("RESULT: Bluetooth permission denied for this process"); exit(3)
        case .unsupported:   print("RESULT: BLE unsupported"); exit(4)
        default:             print("BT state=\(c.state.rawValue)")
        }
    }

    func centralManager(_ c: CBCentralManager, didDiscover p: CBPeripheral,
                        advertisementData: [String: Any], rssi RSSI: NSNumber) {
        guard !seen.contains(p.identifier) else { return }
        seen.insert(p.identifier)
        total += 1
        let name = (advertisementData[CBAdvertisementDataLocalNameKey] as? String) ?? p.name ?? "—"
        var mfgHex = ""
        if let mfg = advertisementData[CBAdvertisementDataManufacturerDataKey] as? Data {
            withMfg += 1
            let b = [UInt8](mfg)
            mfgHex = b.map { String(format:"%02X",$0) }.joined()
            if b.count >= 4, b[0]==0x4C, b[1]==0x00, b[2]==0x02, b[3]==0x15 { ibeacons += 1 }
        }
        let svc = (advertisementData[CBAdvertisementDataServiceUUIDsKey] as? [CBUUID])?.map{$0.uuidString}.joined(separator:",") ?? ""
        print("dev rssi=\(RSSI) name=\"\(name)\" svc=[\(svc)] mfg=\(mfgHex.isEmpty ? "none" : mfgHex)")
    }
}

let s = Scanner()
RunLoop.main.run(until: Date().addingTimeInterval(11))
print("--- scan ended: \(s.total) devices, \(s.withMfg) with mfg-data, \(s.ibeacons) iBeacon-format ---")
