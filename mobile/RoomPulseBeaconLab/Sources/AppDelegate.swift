import UIKit

/// Wires the location delegate at launch so background relaunches (triggered by
/// a beacon region event) deliver the event to a live RoomMonitor.
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        RoomMonitor.shared.bootstrap()
        return true
    }
}
