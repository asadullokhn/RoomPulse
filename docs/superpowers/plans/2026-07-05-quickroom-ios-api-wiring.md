# QuickRoom iOS API Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the QuickRoom iOS app's mock data layer with a real API service against `https://rp.asadullokhn.uz`: server-backed rooms/reservations, Sign in with Apple sessions for booking, and beacon presence events.

**Architecture:** One thin `APIClient` (URLSession + typed errors) feeds three consumers rewired in place: `AuthService` (new, Sign in with Apple → session token in Keychain), `ReservationService` (mock internals → network, public surface unchanged), and `BeaconMonitoringService` (placeholder notifications → `POST /presence`). Static floorplan polygons stay; their ids are re-keyed to real backend workspace ids.

**Tech Stack:** Swift (Xcode 26.6 project, iOS 26 target, `SWIFT_DEFAULT_ACTOR_ISOLATION = MainActor`), SwiftUI, AuthenticationServices, CoreLocation, XCTest. Backend is live — no backend code changes; one VPS env change.

**Spec:** `docs/superpowers/specs/2026-07-05-quickroom-ios-api-wiring-design.md` (this repo).

## Global Constraints

- **Work happens in TWO repos.** iOS work: local clone at `/Users/asadullokhn/CascadeProjects/Personal/QuickRoom` (github `Reishandy/QuickRoom`), branch `feature/api-service` off `main`. Config-mirror work: this repo (`/Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon`). Every `git` command below states which repo it runs in.
- **NEVER add `Co-Authored-By` to any commit.** Concise imperative commit messages, no emojis.
- **Indentation in Swift files: TABS** (match Rei's style). New files get his Xcode header comment style with `Created by Asadullokh Nurullaev on 05/07/26.`
- **Xcode:** every `xcodebuild`/`simctl` command needs `DEVELOPER_DIR=/Applications/Xcode.app` (default xcode-select is CLT-only). Simulator destination: `platform=iOS Simulator,name=iPhone 17`.
- **Build command (used as "the build" everywhere):**
  ```bash
  cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && DEVELOPER_DIR=/Applications/Xcode.app xcodebuild -project QuickRoom.xcodeproj -scheme QuickRoom -destination 'platform=iOS Simulator,name=iPhone 17' -quiet build CODE_SIGNING_ALLOWED=NO
  ```
- **Test command:** same but `test` instead of `build` (after Task 2 adds the scheme + test target).
- **The project uses `PBXFileSystemSynchronizedRootGroup`** — new `.swift` files under `QuickRoom/` (and `QuickRoomTests/` after Task 2) join the build automatically. Never edit the pbxproj to add source files.
- **Concurrency:** the project sets `SWIFT_DEFAULT_ACTOR_ISOLATION = MainActor` + `SWIFT_APPROACHABLE_CONCURRENCY = YES`. Write plain classes/enums (implicitly MainActor); do NOT declare `actor`s or fight isolation. `URLSession.data(for:)` awaits off-main by itself.
- **Backend facts:** dates are RFC 3339 with up to **9 fractional digits** (`2026-07-03T18:23:22.660190936Z`) — `ISO8601DateFormatter` with `.withFractionalSeconds` parses **exactly 3**, so fractions must be truncated before parsing. Error body shape: `{"error": "message"}`. Bearer auth: `Authorization: Bearer <session_token>`.

---

### Task 1: Branch, xcconfig base URL, Sign in with Apple entitlement

**Files:**
- Modify: `Debug.xcconfig`, `Release.xcconfig`, `QuickRoom/QuickRoom.entitlements` (QuickRoom repo)

**Interfaces:**
- Produces: working `AppConfig.API.baseURL == URL("https://rp.asadullokhn.uz")` for all later tasks; SIWA entitlement for Task 4.

- [ ] **Step 1: Create the branch**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git checkout -b feature/api-service
```

- [ ] **Step 2: Baseline build (proves toolchain before any change)**

Run the build command from Global Constraints. Expected: exits 0. If this fails, STOP — the environment is broken, nothing later can be verified.

- [ ] **Step 3: Fix both xcconfigs**

In xcconfig syntax `//` begins a comment, so the current `https://staging.api.example.com` silently truncates to `https:` — `$()` (empty substitution) splices the slashes through. Replace the `API_BASE_URL` line in **both** `Debug.xcconfig` and `Release.xcconfig`:

```
// xcconfig treats // as a comment even inside values; $() splices the URL through.
API_BASE_URL = https:/$()/rp.asadullokhn.uz
```

Keep the existing `BEACON_PROXIMITY_UUID` lines untouched (already correct).

- [ ] **Step 4: Add the Sign in with Apple entitlement**

In `QuickRoom/QuickRoom.entitlements`, add inside the existing `<dict>`:

```xml
	<key>com.apple.developer.applesignin</key>
	<array>
		<string>Default</string>
	</array>
```

- [ ] **Step 5: Verify the URL survives**

Run the build, then:

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && APP=$(find ~/Library/Developer/Xcode/DerivedData -path "*QuickRoom*/Build/Products/Debug-iphonesimulator/QuickRoom.app" -newer Debug.xcconfig | head -1) && /usr/libexec/PlistBuddy -c "Print :API_BASE_URL" "$APP/Info.plist"
```

Expected output: `https://rp.asadullokhn.uz`

- [ ] **Step 6: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add Debug.xcconfig Release.xcconfig QuickRoom/QuickRoom.entitlements && git commit -m "Point API base URL at production backend, add Sign in with Apple entitlement

xcconfig treats // as a comment start, so the previous placeholder URLs
silently truncated to \"https:\" - the \$() splice keeps the slashes."
```

---

### Task 2: Shared scheme + unit-test target

**Files:**
- Create: `QuickRoom.xcodeproj/xcshareddata/xcschemes/QuickRoom.xcscheme`
- Create: `QuickRoomTests/QuickRoomTests.swift`
- Modify: `QuickRoom.xcodeproj/project.pbxproj`

**Interfaces:**
- Produces: `xcodebuild … test` runs XCTest cases from `QuickRoomTests/` (host app `QuickRoom.app`). All later TDD tasks depend on this.
- **Fallback (use only if Step 4 cannot be made green in ~3 attempts):** `git checkout QuickRoom.xcodeproj && git rm -r QuickRoomTests && rm QuickRoom.xcodeproj/xcshareddata/xcschemes/QuickRoom.xcscheme`, then implement remaining tasks without the test steps, verifying by build + the Task 9 live run, and say so in the PR body.

- [ ] **Step 1: Create the test folder + smoke test**

`QuickRoomTests/QuickRoomTests.swift` (tabs):

```swift
//
//  QuickRoomTests.swift
//  QuickRoomTests
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import XCTest
@testable import QuickRoom

final class SmokeTests: XCTestCase {
	func testHarnessRuns() {
		XCTAssertEqual(StaticRooms.rooms.isEmpty, false)
	}
}
```

- [ ] **Step 2: Add the test target to project.pbxproj**

Six edits, each anchored to an existing section. Invented object ids are consistent hex, no collisions with existing `8B…` ids.

(a) After the line `/* End PBXFileReference section */` insert nothing — instead, INSIDE the `PBXFileReference` section (before its `/* End` line) add:

```
		9A0000000000000000000001 /* QuickRoomTests.xctest */ = {isa = PBXFileReference; explicitFileType = wrapper.cfbundle; includeInIndex = 0; path = QuickRoomTests.xctest; sourceTree = BUILT_PRODUCTS_DIR; };
```

(b) Inside the `PBXFileSystemSynchronizedRootGroup` section (before `/* End`) add:

```
		9A0000000000000000000002 /* QuickRoomTests */ = {
			isa = PBXFileSystemSynchronizedRootGroup;
			path = QuickRoomTests;
			sourceTree = "<group>";
		};
```

(c) In the main `PBXGroup` (`8B35E7C62FF5F41400C0F97F`), add to `children` after `8B35E7D12FF5F41400C0F97F /* QuickRoom */,`:

```
				9A0000000000000000000002 /* QuickRoomTests */,
```

and in the `Products` group (`8B35E7D02FF5F41400C0F97F`) `children`, after the `QuickRoom.app` line:

```
				9A0000000000000000000001 /* QuickRoomTests.xctest */,
```

(d) Inside the `PBXNativeTarget` section (before `/* End`) add a second target, plus a `PBXTargetDependency`/`PBXContainerItemProxy` pair in new sections placed directly after the `PBXNativeTarget` section (alphabetical section order is convention, not requirement):

```
		9A0000000000000000000003 /* QuickRoomTests */ = {
			isa = PBXNativeTarget;
			buildConfigurationList = 9A0000000000000000000006 /* Build configuration list for PBXNativeTarget "QuickRoomTests" */;
			buildPhases = (
			);
			buildRules = (
			);
			dependencies = (
				9A0000000000000000000005 /* PBXTargetDependency */,
			);
			fileSystemSynchronizedGroups = (
				9A0000000000000000000002 /* QuickRoomTests */,
			);
			name = QuickRoomTests;
			packageProductDependencies = (
			);
			productName = QuickRoomTests;
			productReference = 9A0000000000000000000001 /* QuickRoomTests.xctest */;
			productType = "com.apple.product-type.bundle.unit-test";
		};
/* End PBXNativeTarget section */

/* Begin PBXContainerItemProxy section */
		9A0000000000000000000004 /* PBXContainerItemProxy */ = {
			isa = PBXContainerItemProxy;
			containerPortal = 8B35E7C72FF5F41400C0F97F /* Project object */;
			proxyType = 1;
			remoteGlobalIDString = 8B35E7CE2FF5F41400C0F97F;
			remoteInfo = QuickRoom;
		};
/* End PBXContainerItemProxy section */

/* Begin PBXTargetDependency section */
		9A0000000000000000000005 /* PBXTargetDependency */ = {
			isa = PBXTargetDependency;
			target = 8B35E7CE2FF5F41400C0F97F /* QuickRoom */;
			targetProxy = 9A0000000000000000000004 /* PBXContainerItemProxy */;
		};
/* End PBXTargetDependency section */
```

(note: the first block replaces the existing `/* End PBXNativeTarget section */` line — i.e. the new target goes before it).

(e) In `PBXProject` → `targets = (`, after the QuickRoom line add:

```
				9A0000000000000000000003 /* QuickRoomTests */,
```

and in `attributes.TargetAttributes` add:

```
					9A0000000000000000000003 = {
						CreatedOnToolsVersion = 26.5;
						TestTargetID = 8B35E7CE2FF5F41400C0F97F;
					};
```

(f) Inside the `XCBuildConfiguration` section (before `/* End`) add two configs, and inside `XCConfigurationList` (before `/* End`) the list:

```
		9A0000000000000000000007 /* Debug */ = {
			isa = XCBuildConfiguration;
			buildSettings = {
				BUNDLE_LOADER = "$(TEST_HOST)";
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				GENERATE_INFOPLIST_FILE = YES;
				IPHONEOS_DEPLOYMENT_TARGET = 26.0;
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = <app-bundle-id>.Tests;
				PRODUCT_NAME = "$(TARGET_NAME)";
				SWIFT_APPROACHABLE_CONCURRENCY = YES;
				SWIFT_DEFAULT_ACTOR_ISOLATION = MainActor;
				SWIFT_EMIT_LOC_STRINGS = NO;
				SWIFT_VERSION = 5.0;
				TARGETED_DEVICE_FAMILY = "1,2";
				TEST_HOST = "$(BUILT_PRODUCTS_DIR)/QuickRoom.app/$(BUNDLE_EXECUTABLE_FOLDER_PATH)/QuickRoom";
			};
			name = Debug;
		};
		9A0000000000000000000008 /* Release */ = {
			isa = XCBuildConfiguration;
			buildSettings = {
				BUNDLE_LOADER = "$(TEST_HOST)";
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				GENERATE_INFOPLIST_FILE = YES;
				IPHONEOS_DEPLOYMENT_TARGET = 26.0;
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = <app-bundle-id>.Tests;
				PRODUCT_NAME = "$(TARGET_NAME)";
				SWIFT_APPROACHABLE_CONCURRENCY = YES;
				SWIFT_DEFAULT_ACTOR_ISOLATION = MainActor;
				SWIFT_EMIT_LOC_STRINGS = NO;
				SWIFT_VERSION = 5.0;
				TARGETED_DEVICE_FAMILY = "1,2";
				TEST_HOST = "$(BUILT_PRODUCTS_DIR)/QuickRoom.app/$(BUNDLE_EXECUTABLE_FOLDER_PATH)/QuickRoom";
			};
			name = Release;
		};
```

```
		9A0000000000000000000006 /* Build configuration list for PBXNativeTarget "QuickRoomTests" */ = {
			isa = XCConfigurationList;
			buildConfigurations = (
				9A0000000000000000000007 /* Debug */,
				9A0000000000000000000008 /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		};
```

Note: NO `DEVELOPMENT_TEAM` in the test configs and `CODE_SIGNING_ALLOWED=NO` on the command line — simulator tests don't sign.

- [ ] **Step 3: Create the shared scheme**

`QuickRoom.xcodeproj/xcshareddata/xcschemes/QuickRoom.xcscheme`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion = "2650" version = "1.7">
   <BuildAction parallelizeBuildables = "YES" buildImplicitDependencies = "YES">
      <BuildActionEntries>
         <BuildActionEntry buildForTesting = "YES" buildForRunning = "YES" buildForProfiling = "YES" buildForArchiving = "YES" buildForAnalyzing = "YES">
            <BuildableReference BuildableState = "YES" BlueprintIdentifier = "8B35E7CE2FF5F41400C0F97F" BuildableName = "QuickRoom.app" BlueprintName = "QuickRoom" ReferencedContainer = "container:QuickRoom.xcodeproj"/>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction buildConfiguration = "Debug" selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB" shouldUseLaunchSchemeArgsEnv = "YES">
      <Testables>
         <TestableReference skipped = "NO">
            <BuildableReference BuildableState = "YES" BlueprintIdentifier = "9A0000000000000000000003" BuildableName = "QuickRoomTests.xctest" BlueprintName = "QuickRoomTests" ReferencedContainer = "container:QuickRoom.xcodeproj"/>
         </TestableReference>
      </Testables>
   </TestAction>
   <LaunchAction buildConfiguration = "Debug" selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB" launchStyle = "0" useCustomWorkingDirectory = "NO" ignoresPersistentStateOnLaunch = "NO" debugDocumentVersioning = "YES" debugServiceExtension = "internal" allowLocationSimulation = "YES">
      <BuildableProductRunnable runnableDebuggingMode = "0">
         <BuildableReference BuildableState = "YES" BlueprintIdentifier = "8B35E7CE2FF5F41400C0F97F" BuildableName = "QuickRoom.app" BlueprintName = "QuickRoom" ReferencedContainer = "container:QuickRoom.xcodeproj"/>
      </BuildableProductRunnable>
   </LaunchAction>
   <ProfileAction buildConfiguration = "Release" shouldUseLaunchSchemeArgsEnv = "YES" savedToolIdentifier = "" useCustomWorkingDirectory = "NO" debugDocumentVersioning = "YES">
      <BuildableProductRunnable runnableDebuggingMode = "0">
         <BuildableReference BuildableState = "YES" BlueprintIdentifier = "8B35E7CE2FF5F41400C0F97F" BuildableName = "QuickRoom.app" BlueprintName = "QuickRoom" ReferencedContainer = "container:QuickRoom.xcodeproj"/>
      </BuildableProductRunnable>
   </ProfileAction>
   <AnalyzeAction buildConfiguration = "Debug"/>
   <ArchiveAction buildConfiguration = "Release" revealArchiveInOrganizer = "YES"/>
</Scheme>
```

- [ ] **Step 4: Run the test suite**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && DEVELOPER_DIR=/Applications/Xcode.app xcodebuild -project QuickRoom.xcodeproj -scheme QuickRoom -destination 'platform=iOS Simulator,name=iPhone 17' test CODE_SIGNING_ALLOWED=NO 2>&1 | tail -20
```

Expected: `Test Suite 'SmokeTests' passed` and `** TEST SUCCEEDED **`. If the pbxproj won't parse (`xcodebuild -list` errors), fix or invoke the fallback in Interfaces.

- [ ] **Step 5: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add QuickRoom.xcodeproj QuickRoomTests && git commit -m "Add QuickRoomTests unit-test target and shared scheme

Shared scheme also unblocks CI/xcodebuild on machines that never opened
the project in Xcode (user schemes are gitignored)."
```

---

### Task 3: DTOs + APIClient

**Files:**
- Create: `QuickRoom/Core/Network/DTOs.swift`, `QuickRoom/Core/Network/APIClient.swift`
- Test: `QuickRoomTests/APIClientTests.swift`

**Interfaces:**
- Consumes: `AppConfig.API.baseURL` (exists), `KeychainStore.sessionToken` (Task 4 — referenced via closure injected in Task 4; APIClient itself stays storage-agnostic).
- Produces (later tasks call exactly these):
  - `final class APIClient` with `static var shared: APIClient` (a `var` so Task 4 can finalize the token provider — see note), `init(baseURL: URL, session: URLSession = .shared, tokenProvider: @escaping () -> String?)`
  - `func get<T: Decodable>(_ path: String) async throws -> T`
  - `func post<T: Decodable, B: Encodable>(_ path: String, body: B) async throws -> T`
  - `func post<T: Decodable>(_ path: String) async throws -> T` (no body)
  - `enum APIError: LocalizedError` with cases `.unauthorized`, `.conflict(String)`, `.server(status: Int, message: String)`, `.transport(Error)`
  - DTOs: `RoomDTO(roomId, zoomWorkspaceId, name, capacity, hasTv, isZoomRoom)`, `RoomsResponse(rooms)`, `ReservationDTO(reservationId, roomId, zoomWorkspaceId, userId, userEmail, startTime: Date, endTime: Date, status, checkInStatus, source, bookedByUserId: String?)`, `ReservationsResponse(reservations)`, `UserDTO(userId, email, name): Codable`, `AuthResponse(sessionToken, user)`, `BeaconEntryDTO(workspaceId, uuid, major: Int, minor: Int, name)`, `BeaconsResponse(beacons)`, `StatusResponse(status: String?, workspaceId: String?)`, request bodies `CreateReservationRequest(workspaceId, startTime: Date, endTime: Date)`, `AppleAuthRequest(identityToken, name: String?)`, `PresenceRequest(workspaceId, userId, displayName, eventType, eventTs: Int64)`

**Note on `APIClient.shared`:** to avoid a circular dependency at static-init time, `shared` starts with `tokenProvider: { KeychainStore.sessionToken }`. `KeychainStore` arrives in Task 4, so in THIS task `shared` uses `{ nil }` and Task 4 changes that one line. This is the only cross-task edit.

- [ ] **Step 1: Write the failing tests**

`QuickRoomTests/APIClientTests.swift`. The date fixture uses the backend's real 9-digit fractional form — `ISO8601DateFormatter.withFractionalSeconds` alone cannot parse it; this test locks in the truncation fix.

```swift
//
//  APIClientTests.swift
//  QuickRoomTests
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import XCTest
@testable import QuickRoom

final class APIClientTests: XCTestCase {
	func testDecodesReservationWithNanosecondDates() throws {
		let json = Data("""
		{"reservations":[{"reservation_id":"res-agung","room_id":"room-ws-agung","zoom_workspace_id":"ws-agung","user_id":"","user_email":"demo.day@adabali.dev","start_time":"2026-07-03T18:23:22.660190936Z","end_time":"2026-07-03T19:53:22.660190936Z","status":"booked","check_in_status":"checked_out","source":"zoom"}]}
		""".utf8)
		let response = try APIClient.decoder.decode(ReservationsResponse.self, from: json)
		let reservation = try XCTUnwrap(response.reservations.first)
		XCTAssertEqual(reservation.reservationId, "res-agung")
		XCTAssertEqual(reservation.zoomWorkspaceId, "ws-agung")
		XCTAssertNil(reservation.bookedByUserId)
		// 2026-07-03T18:23:22.660Z (fraction truncated to millis)
		XCTAssertEqual(reservation.startTime.timeIntervalSince1970, 1783189402.660, accuracy: 0.01)
	}

	func testDecodesPlainSecondDates() throws {
		let json = Data("""
		{"reservations":[{"reservation_id":"r1","room_id":"room-x","zoom_workspace_id":"ws-x","user_id":"u1","user_email":"e","start_time":"2026-07-05T07:00:00Z","end_time":"2026-07-05T08:00:00Z","status":"booked","check_in_status":"not_checked_in","source":"app","booked_by_user_id":"u1"}]}
		""".utf8)
		let response = try APIClient.decoder.decode(ReservationsResponse.self, from: json)
		XCTAssertEqual(response.reservations.first?.bookedByUserId, "u1")
	}

	func testDecodesRoomsBeaconsAuth() throws {
		let rooms = try APIClient.decoder.decode(RoomsResponse.self, from: Data("""
		{"rooms":[{"room_id":"room-ws-agung","zoom_workspace_id":"ws-agung","name":"BINB Agung Zoom","floor":"","capacity":80,"has_tv":true,"is_zoom_room":true}]}
		""".utf8))
		XCTAssertEqual(rooms.rooms.first?.capacity, 80)

		let beacons = try APIClient.decoder.decode(BeaconsResponse.self, from: Data("""
		{"beacons":[{"workspace_id":"ws-agung","uuid":"11111111-2222-3333-4444-555555555555","major":1,"minor":106,"name":"BINB Agung Zoom"}]}
		""".utf8))
		XCTAssertEqual(beacons.beacons.first?.minor, 106)

		let auth = try APIClient.decoder.decode(AuthResponse.self, from: Data("""
		{"session_token":"tok123","user":{"user_id":"u-1","email":"a@b.c","name":"Asadullokh","created_at":"2026-07-05T00:00:00Z"}}
		""".utf8))
		XCTAssertEqual(auth.sessionToken, "tok123")
		XCTAssertEqual(auth.user.userId, "u-1")
	}

	func testEncodesSnakeCaseAndRFC3339() throws {
		let body = CreateReservationRequest(workspaceId: "ws-ubud", startTime: Date(timeIntervalSince1970: 1783328400), endTime: Date(timeIntervalSince1970: 1783332000))
		let json = try XCTUnwrap(String(data: APIClient.encoder.encode(body), encoding: .utf8))
		XCTAssertTrue(json.contains("\"workspace_id\":\"ws-ubud\""), json)
		XCTAssertTrue(json.contains("\"start_time\":\"2026-07-06T05:00:00Z\""), json)
	}

	func testErrorMapping() async throws {
		let client = APIClient(baseURL: URL(string: "https://example.invalid")!, session: StubURLProtocol.session(), tokenProvider: { "tok" })

		StubURLProtocol.respond(status: 401, body: #"{"error":"invalid session"}"#)
		do {
			let _: StatusResponse = try await client.get("/reservations/mine")
			XCTFail("expected throw")
		} catch APIError.unauthorized {
		}

		StubURLProtocol.respond(status: 409, body: #"{"error":"room already booked"}"#)
		do {
			let _: StatusResponse = try await client.post("/reservations", body: CreateReservationRequest(workspaceId: "w", startTime: .now, endTime: .now))
			XCTFail("expected throw")
		} catch APIError.conflict(let message) {
			XCTAssertTrue(message.contains("already booked"))
		}
	}

	func testSendsBearerToken() async throws {
		let client = APIClient(baseURL: URL(string: "https://example.invalid")!, session: StubURLProtocol.session(), tokenProvider: { "secret-token" })
		StubURLProtocol.respond(status: 200, body: #"{"status":"ok"}"#)
		let _: StatusResponse = try await client.get("/health/live")
		XCTAssertEqual(StubURLProtocol.lastRequest?.value(forHTTPHeaderField: "Authorization"), "Bearer secret-token")
	}
}

final class StubURLProtocol: URLProtocol {
	nonisolated(unsafe) static var stubStatus = 200
	nonisolated(unsafe) static var stubBody = Data()
	nonisolated(unsafe) static var lastRequest: URLRequest?

	static func respond(status: Int, body: String) {
		stubStatus = status
		stubBody = Data(body.utf8)
	}

	static func session() -> URLSession {
		let config = URLSessionConfiguration.ephemeral
		config.protocolClasses = [StubURLProtocol.self]
		return URLSession(configuration: config)
	}

	override class func canInit(with request: URLRequest) -> Bool { true }
	override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

	override func startLoading() {
		Self.lastRequest = request
		let response = HTTPURLResponse(url: request.url!, statusCode: Self.stubStatus, httpVersion: nil, headerFields: nil)!
		client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
		client?.urlProtocol(self, didLoad: Self.stubBody)
		client?.urlProtocolDidFinishLoading(self)
	}

	override func stopLoading() {}
}
```

- [ ] **Step 2: Run tests, verify they fail to compile** (missing types)

Run the test command. Expected: build FAILS with "cannot find 'APIClient' in scope" (a compile failure is this cycle's red).

- [ ] **Step 3: Implement DTOs**

`QuickRoom/Core/Network/DTOs.swift`:

```swift
//
//  DTOs.swift
//  QuickRoom
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import Foundation

struct RoomsResponse: Decodable {
	let rooms: [RoomDTO]
}

struct RoomDTO: Decodable {
	let roomId: String
	let zoomWorkspaceId: String
	let name: String
	let capacity: Int
	let hasTv: Bool
	let isZoomRoom: Bool
}

struct ReservationsResponse: Decodable {
	let reservations: [ReservationDTO]
}

struct ReservationDTO: Decodable {
	let reservationId: String
	let roomId: String
	let zoomWorkspaceId: String
	let userId: String
	let userEmail: String
	let startTime: Date
	let endTime: Date
	let status: String
	let checkInStatus: String
	let source: String
	let bookedByUserId: String?
}

struct UserDTO: Codable {
	let userId: String
	let email: String
	let name: String
}

struct AuthResponse: Decodable {
	let sessionToken: String
	let user: UserDTO
}

struct BeaconsResponse: Decodable {
	let beacons: [BeaconEntryDTO]
}

struct BeaconEntryDTO: Decodable {
	let workspaceId: String
	let uuid: String
	let major: Int
	let minor: Int
	let name: String
}

/// Generic `{"status":"ok"}`-style responses; also absorbs POST /presence,
/// which returns either a status object or a full reservation.
struct StatusResponse: Decodable {
	let status: String?
	let workspaceId: String?
}

struct CreateReservationRequest: Encodable {
	let workspaceId: String
	let startTime: Date
	let endTime: Date
}

struct AppleAuthRequest: Encodable {
	let identityToken: String
	let name: String?
}

struct PresenceRequest: Encodable {
	let workspaceId: String
	let userId: String
	let displayName: String
	let eventType: String
	let eventTs: Int64
}
```

- [ ] **Step 4: Implement APIClient**

`QuickRoom/Core/Network/APIClient.swift`:

```swift
//
//  APIClient.swift
//  QuickRoom
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import Foundation

enum APIError: LocalizedError {
	case unauthorized
	case conflict(String)
	case server(status: Int, message: String)
	case transport(Error)

	var errorDescription: String? {
		switch self {
		case .unauthorized: return "Sign in to book rooms."
		case .conflict(let message): return message
		case .server(_, let message): return message
		case .transport: return "Network error. Check your connection."
		}
	}
}

final class APIClient {
	// Task 4 swaps the token provider to read the Keychain session.
	static var shared = APIClient(baseURL: AppConfig.API.baseURL, tokenProvider: { nil })

	private let baseURL: URL
	private let session: URLSession
	private let tokenProvider: () -> String?

	init(baseURL: URL, session: URLSession = .shared, tokenProvider: @escaping () -> String?) {
		self.baseURL = baseURL
		self.session = session
		self.tokenProvider = tokenProvider
	}

	func get<T: Decodable>(_ path: String) async throws -> T {
		try await send("GET", path, body: Optional<Never>.none)
	}

	func post<T: Decodable, B: Encodable>(_ path: String, body: B) async throws -> T {
		try await send("POST", path, body: body)
	}

	func post<T: Decodable>(_ path: String) async throws -> T {
		try await send("POST", path, body: Optional<Never>.none)
	}

	private func send<T: Decodable, B: Encodable>(_ method: String, _ path: String, body: B?) async throws -> T {
		var request = URLRequest(url: baseURL.appending(path: path))
		request.httpMethod = method
		if let body {
			request.httpBody = try Self.encoder.encode(body)
			request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		}
		if let token = tokenProvider() {
			request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
		}

		let data: Data
		let response: URLResponse
		do {
			(data, response) = try await session.data(for: request)
		} catch {
			throw APIError.transport(error)
		}

		let status = (response as? HTTPURLResponse)?.statusCode ?? 0
		guard (200..<300).contains(status) else {
			let message = (try? Self.decoder.decode(ServerErrorBody.self, from: data))?.error ?? "Request failed (\(status))"
			switch status {
			case 401: throw APIError.unauthorized
			case 409: throw APIError.conflict(message)
			default: throw APIError.server(status: status, message: message)
			}
		}
		return try Self.decoder.decode(T.self, from: data)
	}

	private struct ServerErrorBody: Decodable {
		let error: String
	}

	static let decoder: JSONDecoder = {
		let decoder = JSONDecoder()
		decoder.keyDecodingStrategy = .convertFromSnakeCase
		decoder.dateDecodingStrategy = .custom { decoder in
			let container = try decoder.singleValueContainer()
			let raw = try container.decode(String.self)
			guard let date = parseRFC3339(raw) else {
				throw DecodingError.dataCorruptedError(in: container, debugDescription: "unparseable date \(raw)")
			}
			return date
		}
		return decoder
	}()

	static let encoder: JSONEncoder = {
		let encoder = JSONEncoder()
		encoder.keyEncodingStrategy = .convertToSnakeCase
		encoder.dateEncodingStrategy = .iso8601
		return encoder
	}()

	private static let isoPlain: ISO8601DateFormatter = {
		let formatter = ISO8601DateFormatter()
		formatter.formatOptions = [.withInternetDateTime]
		return formatter
	}()

	private static let isoFractional: ISO8601DateFormatter = {
		let formatter = ISO8601DateFormatter()
		formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
		return formatter
	}()

	/// Go emits RFC 3339 with up to nanosecond precision, but
	/// ISO8601DateFormatter's fractional mode parses exactly three digits —
	/// so truncate the fraction to milliseconds before parsing.
	static func parseRFC3339(_ raw: String) -> Date? {
		if let date = isoPlain.date(from: raw) {
			return date
		}
		guard let dotIndex = raw.firstIndex(of: ".") else { return nil }
		var fractionEnd = raw.index(after: dotIndex)
		while fractionEnd < raw.endIndex, raw[fractionEnd].isNumber {
			fractionEnd = raw.index(after: fractionEnd)
		}
		let fraction = raw[raw.index(after: dotIndex)..<fractionEnd].prefix(3)
		let trimmed = raw[..<dotIndex] + "." + fraction + raw[fractionEnd...]
		return isoFractional.date(from: String(trimmed))
	}
}

extension Never: @retroactive Encodable {
	public func encode(to encoder: Encoder) throws {}
}
```

(If the compiler rejects the `Never: Encodable` retroactive conformance — iOS 26 SDK may already declare it — delete that extension; nothing else changes.)

- [ ] **Step 5: Run tests, verify green**

Run the test command. Expected: `** TEST SUCCEEDED **`, all APIClientTests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add QuickRoom/Core/Network QuickRoomTests/APIClientTests.swift && git commit -m "Add APIClient and backend DTOs

Backend dates carry nanosecond fractions which ISO8601DateFormatter
cannot parse; the client truncates to milliseconds before parsing."
```

---

### Task 4: KeychainStore + AuthService

**Files:**
- Create: `QuickRoom/Core/Services/KeychainStore.swift`, `QuickRoom/Core/Services/AuthService.swift`
- Modify: `QuickRoom/Core/Network/APIClient.swift` (one line: token provider)
- Test: `QuickRoomTests/AuthServiceTests.swift`

**Interfaces:**
- Consumes: `APIClient.post`, `AuthResponse`, `UserDTO`, `AppleAuthRequest`, `StatusResponse` (Task 3).
- Produces:
  - `enum KeychainStore` with `static var sessionToken: String?` and `static var currentUserJSON: String?` (get/set, set nil deletes)
  - `@Observable final class AuthService` with `static let shared`, `init(client: APIClient = .shared)`, `private(set) var currentUser: UserDTO?`, `var isSignedIn: Bool`, `func configure(_ request: ASAuthorizationAppleIDRequest)`, `func completeSignIn(_ result: Result<ASAuthorization, Error>) async throws`, `func signOut() async`

- [ ] **Step 1: Write the failing tests**

`QuickRoomTests/AuthServiceTests.swift`:

```swift
//
//  AuthServiceTests.swift
//  QuickRoomTests
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import XCTest
@testable import QuickRoom

final class AuthServiceTests: XCTestCase {
	override func tearDown() {
		KeychainStore.sessionToken = nil
		KeychainStore.currentUserJSON = nil
		super.tearDown()
	}

	func testKeychainRoundtrip() {
		KeychainStore.sessionToken = "tok-abc"
		XCTAssertEqual(KeychainStore.sessionToken, "tok-abc")
		KeychainStore.sessionToken = "tok-replaced"
		XCTAssertEqual(KeychainStore.sessionToken, "tok-replaced")
		KeychainStore.sessionToken = nil
		XCTAssertNil(KeychainStore.sessionToken)
	}

	func testAuthServiceRestoresPersistedSession() {
		KeychainStore.sessionToken = "tok-abc"
		KeychainStore.currentUserJSON = #"{"userId":"u-1","email":"a@b.c","name":"Asadullokh"}"#
		let service = AuthService(client: .shared)
		XCTAssertTrue(service.isSignedIn)
		XCTAssertEqual(service.currentUser?.userId, "u-1")
	}

	func testAuthServiceWithoutTokenIsSignedOut() {
		let service = AuthService(client: .shared)
		XCTAssertFalse(service.isSignedIn)
		XCTAssertNil(service.currentUser)
	}
}
```

- [ ] **Step 2: Run tests, verify compile failure** ("cannot find 'KeychainStore'")

- [ ] **Step 3: Implement KeychainStore**

`QuickRoom/Core/Services/KeychainStore.swift`:

```swift
//
//  KeychainStore.swift
//  QuickRoom
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import Foundation
import Security

enum KeychainStore {
	private static let service = "<app-bundle-id>"

	static var sessionToken: String? {
		get { string(for: "session_token") }
		set { setString(newValue, for: "session_token") }
	}

	static var currentUserJSON: String? {
		get { string(for: "current_user") }
		set { setString(newValue, for: "current_user") }
	}

	private static func string(for key: String) -> String? {
		var query = baseQuery(for: key)
		query[kSecReturnData as String] = true
		query[kSecMatchLimit as String] = kSecMatchLimitOne
		var item: CFTypeRef?
		guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
			  let data = item as? Data else { return nil }
		return String(data: data, encoding: .utf8)
	}

	private static func setString(_ value: String?, for key: String) {
		SecItemDelete(baseQuery(for: key) as CFDictionary)
		guard let value else { return }
		var query = baseQuery(for: key)
		query[kSecValueData as String] = Data(value.utf8)
		SecItemAdd(query as CFDictionary, nil)
	}

	private static func baseQuery(for key: String) -> [String: Any] {
		[
			kSecClass as String: kSecClassGenericPassword,
			kSecAttrService as String: service,
			kSecAttrAccount as String: key,
		]
	}
}
```

- [ ] **Step 4: Implement AuthService**

`QuickRoom/Core/Services/AuthService.swift`:

```swift
//
//  AuthService.swift
//  QuickRoom
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import AuthenticationServices
import Foundation
import SwiftUI

@Observable
final class AuthService {
	static let shared = AuthService()

	private(set) var currentUser: UserDTO?

	var isSignedIn: Bool { currentUser != nil }

	private let client: APIClient

	init(client: APIClient = .shared) {
		self.client = client
		if KeychainStore.sessionToken != nil, let json = KeychainStore.currentUserJSON {
			currentUser = try? JSONDecoder().decode(UserDTO.self, from: Data(json.utf8))
		}
	}

	func configure(_ request: ASAuthorizationAppleIDRequest) {
		request.requestedScopes = [.fullName, .email]
	}

	func completeSignIn(_ result: Result<ASAuthorization, Error>) async throws {
		guard case .success(let authorization) = result,
			  let credential = authorization.credential as? ASAuthorizationAppleIDCredential,
			  let tokenData = credential.identityToken,
			  let identityToken = String(data: tokenData, encoding: .utf8) else {
			throw APIError.server(status: 0, message: "Apple sign-in was cancelled or failed.")
		}

		// Apple sends the name only on the very first authorization.
		var name: String?
		if let components = credential.fullName {
			let formatted = PersonNameComponentsFormatter().string(from: components)
			name = formatted.isEmpty ? nil : formatted
		}

		let response: AuthResponse = try await client.post("/auth/apple", body: AppleAuthRequest(identityToken: identityToken, name: name))
		KeychainStore.sessionToken = response.sessionToken
		if let data = try? JSONEncoder().encode(response.user) {
			KeychainStore.currentUserJSON = String(data: data, encoding: .utf8)
		}
		currentUser = response.user
	}

	func signOut() async {
		let _: StatusResponse? = try? await client.post("/auth/logout")
		KeychainStore.sessionToken = nil
		KeychainStore.currentUserJSON = nil
		currentUser = nil
	}
}
```

- [ ] **Step 5: Point APIClient.shared at the Keychain**

In `QuickRoom/Core/Network/APIClient.swift` replace the `shared` line:

```swift
	static var shared = APIClient(baseURL: AppConfig.API.baseURL, tokenProvider: { KeychainStore.sessionToken })
```

- [ ] **Step 6: Run tests, verify green** (`** TEST SUCCEEDED **`)

- [ ] **Step 7: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add QuickRoom/Core/Services/KeychainStore.swift QuickRoom/Core/Services/AuthService.swift QuickRoom/Core/Network/APIClient.swift QuickRoomTests/AuthServiceTests.swift && git commit -m "Add Sign in with Apple auth service with Keychain-backed session"
```

---

### Task 5: StaticRooms re-key + ReservationService network rewrite

**Files:**
- Modify: `QuickRoom/Data/Static/StaticRooms.swift` (re-key ids/names, drop 11th polygon)
- Modify: `QuickRoom/Core/Services/ReservationService.swift` (full rewrite of internals)
- Test: `QuickRoomTests/ReservationServiceTests.swift`

**Interfaces:**
- Consumes: `APIClient.get/post`, DTOs (Task 3), `AuthService.shared.currentUser` (Task 4).
- Produces: `ReservationService` with the SAME public surface the views already use: `var rooms: [Room]`, `var reservations: [Reservation]`, `var isLoading: Bool`, `init()` (defaulted args), `func fetchReservationsOnLoad() async throws`, `func reserve(roomId:startTime:endTime:) async throws`, `func cancelReservation(reservationId:) async throws`, `func status(for:at:) -> RoomStatus`. Plus testable statics: `static func overlayServerRooms(onto:server:) -> (rooms: [Room], serverBacked: Set<String>)`, `static func mapReservations(_:myUserId:) -> [Reservation]`. Room ids everywhere are now backend workspace ids (`ws-agung`, …).

- [ ] **Step 1: Re-key StaticRooms**

Replace the entire room list in `QuickRoom/Data/Static/StaticRooms.swift` — same polygons, real ids/names, size-matched (biggest polygon → 80-person Agung; the three right-column shapes → the 4-person non-Zoom workspaces). `room-k` (the last right-column shape at y 0.70–0.83) is dropped: 11 polygons, 10 real rooms. Rei can re-assign any of this by editing one file.

```swift
	static let rooms: [Room] = [
		// Mapping to backend workspaces is size-matched and demo-arbitrary;
		// re-key here if beacons/rooms move.
		Room(id: "ws-agung", name: "BINB Agung Zoom", relativePoints: [
			CGPoint(x: 0.04, y: 0.09), CGPoint(x: 0.28, y: 0.09),
			CGPoint(x: 0.28, y: 0.79), CGPoint(x: 0.04, y: 0.79)
		]),

		Room(id: "ws-bedugul", name: "BINB Bedugul Zoom", relativePoints: [
			CGPoint(x: 0.33, y: 0.08), CGPoint(x: 0.46, y: 0.08),
			CGPoint(x: 0.46, y: 0.32), CGPoint(x: 0.33, y: 0.32)
		]),
		Room(id: "ws-mengwi", name: "BINB Mengwi Zoom", relativePoints: [
			CGPoint(x: 0.47, y: 0.08), CGPoint(x: 0.59, y: 0.08),
			CGPoint(x: 0.59, y: 0.32), CGPoint(x: 0.47, y: 0.32)
		]),
		Room(id: "ws-nusadua", name: "BINB Nusa Dua Zoom", relativePoints: [
			CGPoint(x: 0.60, y: 0.08), CGPoint(x: 0.72, y: 0.08),
			CGPoint(x: 0.72, y: 0.32), CGPoint(x: 0.60, y: 0.32)
		]),
		Room(id: "ws-petang", name: "BINB Petang Zoom", relativePoints: [
			CGPoint(x: 0.73, y: 0.08), CGPoint(x: 0.86, y: 0.08),
			CGPoint(x: 0.86, y: 0.32), CGPoint(x: 0.73, y: 0.32)
		]),

		Room(id: "ws-sanur", name: "BINB Sanur Zoom", relativePoints: [
			CGPoint(x: 0.29, y: 0.55), CGPoint(x: 0.42, y: 0.55),
			CGPoint(x: 0.42, y: 0.83), CGPoint(x: 0.29, y: 0.83)
		]),
		Room(id: "ws-ubud", name: "BINB Ubud Zoom", relativePoints: [
			CGPoint(x: 0.42, y: 0.55), CGPoint(x: 0.56, y: 0.55),
			CGPoint(x: 0.56, y: 0.83), CGPoint(x: 0.42, y: 0.83)
		]),
		Room(id: "ws-ceningan", name: "Ceningan", relativePoints: [
			CGPoint(x: 0.56, y: 0.55), CGPoint(x: 0.72, y: 0.55),
			CGPoint(x: 0.72, y: 0.83), CGPoint(x: 0.56, y: 0.83)
		]),

		Room(id: "ws-lembongan", name: "Lembongan", relativePoints: [
			CGPoint(x: 0.77, y: 0.44), CGPoint(x: 0.97, y: 0.44),
			CGPoint(x: 0.97, y: 0.57), CGPoint(x: 0.77, y: 0.57)
		]),
		Room(id: "ws-penida", name: "Penida", relativePoints: [
			CGPoint(x: 0.77, y: 0.57), CGPoint(x: 0.97, y: 0.57),
			CGPoint(x: 0.97, y: 0.70), CGPoint(x: 0.77, y: 0.70)
		])
	]
```

- [ ] **Step 2: Write the failing tests**

`QuickRoomTests/ReservationServiceTests.swift`:

```swift
//
//  ReservationServiceTests.swift
//  QuickRoomTests
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import XCTest
@testable import QuickRoom

final class ReservationServiceTests: XCTestCase {
	private func makeReservationDTO(id: String = "r1", status: String = "booked", bookedBy: String? = nil) -> ReservationDTO {
		try! APIClient.decoder.decode(ReservationDTO.self, from: Data("""
		{"reservation_id":"\(id)","room_id":"room-ws-ubud","zoom_workspace_id":"ws-ubud","user_id":"","user_email":"e","start_time":"2026-07-05T07:00:00Z","end_time":"2026-07-05T08:00:00Z","status":"\(status)","check_in_status":"not_checked_in","source":"app"\(bookedBy.map { #","booked_by_user_id":"\($0)""# } ?? "")}
		""".utf8))
	}

	func testOverlayUsesServerNamesAndTracksBacking() {
		let server = try! APIClient.decoder.decode(RoomsResponse.self, from: Data("""
		{"rooms":[{"room_id":"room-ws-agung","zoom_workspace_id":"ws-agung","name":"Agung (Renamed)","floor":"","capacity":80,"has_tv":true,"is_zoom_room":true}]}
		""".utf8)).rooms
		let result = ReservationService.overlayServerRooms(onto: StaticRooms.rooms, server: server)
		XCTAssertEqual(result.rooms.first(where: { $0.id == "ws-agung" })?.name, "Agung (Renamed)")
		XCTAssertEqual(result.serverBacked, ["ws-agung"])
		// Unknown-to-server rooms keep their static name and polygons.
		XCTAssertEqual(result.rooms.first(where: { $0.id == "ws-ubud" })?.name, "BINB Ubud Zoom")
		XCTAssertEqual(result.rooms.count, StaticRooms.rooms.count)
	}

	func testMapReservationsFiltersToBookedAndMarksMine() {
		let dtos = [
			makeReservationDTO(id: "mine", bookedBy: "u-1"),
			makeReservationDTO(id: "theirs", bookedBy: "u-2"),
			makeReservationDTO(id: "cancelled", status: "cancelled", bookedBy: "u-1"),
			makeReservationDTO(id: "released", status: "released"),
		]
		let mapped = ReservationService.mapReservations(dtos, myUserId: "u-1")
		XCTAssertEqual(mapped.map(\.id).sorted(), ["mine", "theirs"])
		XCTAssertEqual(mapped.first(where: { $0.id == "mine" })?.isMyReservation, true)
		XCTAssertEqual(mapped.first(where: { $0.id == "theirs" })?.isMyReservation, false)
		XCTAssertEqual(mapped.first?.roomId, "ws-ubud")
	}

	func testMapReservationsWithNoUserMarksNothingMine() {
		let mapped = ReservationService.mapReservations([makeReservationDTO(bookedBy: "u-1")], myUserId: nil)
		XCTAssertEqual(mapped.first?.isMyReservation, false)
	}

	func testStatusDisabledForUnbackedRoom() {
		let service = ReservationService()
		service.serverBacked = ["ws-agung"]
		let ubud = StaticRooms.rooms.first(where: { $0.id == "ws-ubud" })!
		// 10:00 is inside working hours; the room is unbacked so still disabled.
		let workingHour = Calendar.current.date(bySettingHour: 10, minute: 0, second: 0, of: .now)!
		if case .disabled = service.status(for: ubud, at: workingHour) {} else {
			XCTFail("expected .disabled for room missing from the server")
		}
	}
}
```

- [ ] **Step 3: Run tests, verify compile failure** ("has no member 'overlayServerRooms'")

- [ ] **Step 4: Rewrite ReservationService**

Replace the whole body of `QuickRoom/Core/Services/ReservationService.swift` (keep his header comment):

```swift
import Foundation
import SwiftUI
import UIKit

@Observable
class ReservationService {
	var rooms: [Room] = []
	var reservations: [Reservation] = []
	var isLoading: Bool = false
	var serverBacked: Set<String> = []

	private let client: APIClient
	private let auth: AuthService
	private var refreshTask: Task<Void, Never>?

	init(client: APIClient = .shared, auth: AuthService = .shared) {
		self.client = client
		self.auth = auth
		self.rooms = StaticRooms.rooms
	}

	func fetchReservationsOnLoad() async throws {
		isLoading = true
		defer { isLoading = false }
		try await refresh()
		startAutoRefresh()
	}

	func reserve(roomId: String, startTime: Date, endTime: Date) async throws {
		let _: ReservationDTO = try await client.post("/reservations", body: CreateReservationRequest(workspaceId: roomId, startTime: startTime, endTime: endTime))
		try await refresh()
	}

	func cancelReservation(reservationId: String) async throws {
		let _: ReservationDTO = try await client.post("/reservations/\(reservationId)/cancel")
		try await refresh()
	}

	func status(for room: Room, at time: Date) -> RoomStatus {
		guard serverBacked.contains(room.id) else {
			return .disabled
		}
		guard Calendar.current.isWithinWorkingHours(time) else {
			return .disabled
		}

		let activeReservation = reservations.first { reservation in
			reservation.roomId == room.id && time >= reservation.startTime && time < reservation.endTime
		}

		if let reservation = activeReservation {
			return .reserved(isMine: reservation.isMyReservation)
		}

		return .available
	}

	private func refresh() async throws {
		async let roomsResponse: RoomsResponse = client.get("/rooms")
		async let reservationsResponse: ReservationsResponse = client.get("/reservations")
		let (serverRooms, serverReservations) = try await (roomsResponse.rooms, reservationsResponse.reservations)

		let overlay = Self.overlayServerRooms(onto: StaticRooms.rooms, server: serverRooms)
		rooms = overlay.rooms
		serverBacked = overlay.serverBacked
		reservations = Self.mapReservations(serverReservations, myUserId: auth.currentUser?.userId)
	}

	/// Static polygons + live server names. A mapped room the server no
	/// longer reports stays visible but renders disabled via `serverBacked`.
	static func overlayServerRooms(onto staticRooms: [Room], server: [RoomDTO]) -> (rooms: [Room], serverBacked: Set<String>) {
		let byWorkspaceId = Dictionary(uniqueKeysWithValues: server.map { ($0.zoomWorkspaceId, $0) })
		let rooms = staticRooms.map { room in
			guard let dto = byWorkspaceId[room.id] else { return room }
			return Room(id: room.id, name: dto.name, relativePoints: room.relativePoints)
		}
		return (rooms, Set(staticRooms.map(\.id).filter { byWorkspaceId[$0] != nil }))
	}

	/// Only `booked` reservations block a room; no-shows, releases and
	/// cancellations free it.
	static func mapReservations(_ dtos: [ReservationDTO], myUserId: String?) -> [Reservation] {
		dtos.filter { $0.status == "booked" }.map { dto in
			Reservation(
				id: dto.reservationId,
				roomId: dto.zoomWorkspaceId,
				isMyReservation: myUserId != nil && dto.bookedByUserId == myUserId,
				startTime: dto.startTime,
				endTime: dto.endTime
			)
		}
	}

	/// Other users' bookings and the backend's no-show releases should show
	/// up without a relaunch.
	private func startAutoRefresh() {
		guard refreshTask == nil else { return }
		refreshTask = Task { [weak self] in
			while !Task.isCancelled {
				try? await Task.sleep(for: .seconds(30))
				guard let self else { return }
				if UIApplication.shared.applicationState == .active {
					try? await self.refresh()
				}
			}
		}
	}
}
```

Delete `generateInitialMockData()` and both `// TODO: Replace with network` / `// TODO: Remove` markers — they're done. (`// TODO: Reservation rule` stays: it's Rei's note about booking rules, still open.)

- [ ] **Step 5: Run tests, verify green** (`** TEST SUCCEEDED **`)

- [ ] **Step 6: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add QuickRoom/Data/Static/StaticRooms.swift QuickRoom/Core/Services/ReservationService.swift QuickRoomTests/ReservationServiceTests.swift && git commit -m "Wire ReservationService to the backend, re-key floorplan rooms to workspace ids

Floorplan polygons stay local; ids/names now match real backend
workspaces so bookings target actual rooms. Only status==booked blocks a
room. Auto-refreshes every 30s while foregrounded."
```

---

### Task 6: UI wiring — app environment, onboarding sign-in, reserve errors

**Files:**
- Modify: `QuickRoom/App/QuickRoomApp.swift`, `QuickRoom/UI/Onboarding/OnboardingView.swift`, `QuickRoom/UI/Reserve/ReserveSheetView.swift`, `QuickRoom/UI/Main/ContentView.swift` (preview only)

**Interfaces:**
- Consumes: `AuthService` (Task 4), `APIError.unauthorized` (Task 3), `ReservationService` (Task 5).
- Produces: sign-in reachable from onboarding and from the reserve flow; reserve/cancel failures visible instead of `try?`-swallowed.

- [ ] **Step 1: Inject AuthService in QuickRoomApp**

In `QuickRoom/App/QuickRoomApp.swift` add a state property after the existing ones, and the environment line after `.environment(reservationService)`:

```swift
	@State var authService = AuthService.shared
```

```swift
				.environment(authService)
```

- [ ] **Step 2: Onboarding sign-in step**

Replace the body of `QuickRoom/UI/Onboarding/OnboardingView.swift` (keep header; keep Rei's `// TODO: Onboarding view UI` — the screen is still his to design):

```swift
import SwiftUI
import AuthenticationServices

struct OnboardingView: View {
	@Environment(PreferenceService.self) private var preferenceService
	@Environment(AuthService.self) private var authService

	@State private var signInErrorMessage: String?

	// TODO: Onboarding view UI
	var body: some View {
		VStack(spacing: 16) {
			Text("This is onboarding")

			if authService.isSignedIn {
				Text("Signed in as \(authService.currentUser?.name ?? "")")
					.font(.subheadline)
					.foregroundStyle(.secondary)
			} else {
				SignInWithAppleButton(.signIn) { request in
					authService.configure(request)
				} onCompletion: { result in
					Task {
						do {
							try await authService.completeSignIn(result)
							preferenceService.hasSeenOnboarding = true
						} catch {
							signInErrorMessage = error.localizedDescription
						}
					}
				}
				.signInWithAppleButtonStyle(.black)
				.frame(height: 50)
				.padding(.horizontal, 40)
			}

			Button(authService.isSignedIn ? "Continue" : "Skip for now") {
				preferenceService.hasSeenOnboarding = true
			}
			.buttonStyle(.borderedProminent)
		}
		.alert("Sign-in failed", isPresented: Binding(
			get: { signInErrorMessage != nil },
			set: { if !$0 { signInErrorMessage = nil } }
		)) {
			Button("OK", role: .cancel) {}
		} message: {
			Text(signInErrorMessage ?? "")
		}
	}
}

#Preview {
	OnboardingView()
		.environment(PreferenceService())
		.environment(AuthService.shared)
}
```

- [ ] **Step 3: Reserve sheet — surface errors, prompt sign-in on 401**

In `QuickRoom/UI/Reserve/ReserveSheetView.swift`:

Add after the existing imports: `import AuthenticationServices`.
Add environment + state after the existing properties:

```swift
	@Environment(AuthService.self) private var authService
	@State private var errorMessage: String?
	@State private var isSignInPresented = false
```

Replace the Reserve button's `Task { … }` body:

```swift
						Task {
							isProcessing = true
							defer { isProcessing = false }
							do {
								try await reservationService.reserve(roomId: roomId, startTime: startTime, endTime: endTime)
							} catch APIError.unauthorized {
								isSignInPresented = true
							} catch {
								errorMessage = error.localizedDescription
							}
						}
```

Replace the Cancel button's `Task { … }` body:

```swift
									Task {
										do {
											try await reservationService.cancelReservation(reservationId: reservation.id)
										} catch {
											errorMessage = error.localizedDescription
										}
									}
```

Add these modifiers after `.navigationBarTitleDisplayMode(.inline)`:

```swift
			.alert("Couldn't complete that", isPresented: Binding(
				get: { errorMessage != nil },
				set: { if !$0 { errorMessage = nil } }
			)) {
				Button("OK", role: .cancel) {}
			} message: {
				Text(errorMessage ?? "")
			}
			.sheet(isPresented: $isSignInPresented) {
				VStack(spacing: 16) {
					Text("Sign in to book rooms")
						.font(.headline)
					SignInWithAppleButton(.signIn) { request in
						authService.configure(request)
					} onCompletion: { result in
						Task {
							do {
								try await authService.completeSignIn(result)
								isSignInPresented = false
							} catch {
								errorMessage = error.localizedDescription
								isSignInPresented = false
							}
						}
					}
					.signInWithAppleButtonStyle(.black)
					.frame(height: 50)
					.padding(.horizontal, 40)
				}
				.presentationDetents([.height(200)])
			}
```

Update the preview to add `.environment(AuthService.shared)` and use a real id:

```swift
#Preview {
	ReserveSheetView(roomId: "ws-agung")
		.environment(ReservationService())
		.environment(AuthService.shared)
}
```

- [ ] **Step 4: Fix remaining stale previews/ids**

- `QuickRoom/UI/Main/ContentView.swift` preview: add `.environment(AuthService.shared)` after `.environment(ReservationService())`.
- `QuickRoom/UI/Reserve/ReserveView.swift` preview: change `"room-a"` → `"ws-agung"`.

- [ ] **Step 5: Build + full test suite green** (build command, then test command)

- [ ] **Step 6: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add QuickRoom/App/QuickRoomApp.swift QuickRoom/UI && git commit -m "Add Sign in with Apple to onboarding and reserve flow, surface booking errors

Reserve/cancel no longer swallow failures with try?; a 401 opens a
sign-in sheet instead of failing silently."
```

---

### Task 7: Beacon presence — directory lookup + enter/exit events

**Files:**
- Create: `QuickRoom/Core/Network/BeaconDirectory.swift`, `QuickRoom/Core/Network/PresenceReporter.swift`
- Modify: `QuickRoom/Core/Services/BeaconMonitoringService.swift`, `QuickRoom/App/QuickRoomApp.swift`
- Test: `QuickRoomTests/PresenceTests.swift`

**Interfaces:**
- Consumes: `APIClient`, `BeaconsResponse`, `PresenceRequest`, `StatusResponse` (Task 3), `AuthService.shared.currentUser` (Task 4).
- Produces:
  - `final class BeaconDirectory` with `static let shared`, `init(client: APIClient = .shared)`, `func workspaceId(major: Int, minor: Int) async -> String?`, `func refresh() async`, `static func cacheKey(major: Int, minor: Int) -> String`
  - `final class PresenceReporter` with `init(client: APIClient = .shared, directory: BeaconDirectory = .shared)`, `func reportEnter(major: Int, minor: Int) async`, `func reportExit() async`, `static func identity() -> (userId: String, displayName: String)`

- [ ] **Step 1: Write the failing tests**

`QuickRoomTests/PresenceTests.swift`:

```swift
//
//  PresenceTests.swift
//  QuickRoomTests
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import XCTest
@testable import QuickRoom

final class PresenceTests: XCTestCase {
	func testCacheKeyFormat() {
		XCTAssertEqual(BeaconDirectory.cacheKey(major: 1, minor: 106), "1/106")
	}

	func testPresenceResponseDecodesBothShapes() throws {
		// Status shape (no reservation in the room)
		let status = try APIClient.decoder.decode(StatusResponse.self, from: Data(#"{"status":"recorded","workspace_id":"ws-agung"}"#.utf8))
		XCTAssertEqual(status.status, "recorded")
		// Reservation shape (presence drove a check-in) — must not throw
		let reservation = try APIClient.decoder.decode(StatusResponse.self, from: Data(#"{"reservation_id":"r1","zoom_workspace_id":"ws-agung","status":"booked","check_in_status":"checked_in"}"#.utf8))
		XCTAssertEqual(reservation.status, "booked")
	}

	func testIdentityFallsBackToDeviceWhenSignedOut() {
		KeychainStore.sessionToken = nil
		KeychainStore.currentUserJSON = nil
		let identity = PresenceReporter.identity()
		XCTAssertFalse(identity.userId.isEmpty)
		XCTAssertFalse(identity.displayName.isEmpty)
	}
}
```

- [ ] **Step 2: Run tests, verify compile failure** ("cannot find 'BeaconDirectory'")

- [ ] **Step 3: Implement BeaconDirectory**

`QuickRoom/Core/Network/BeaconDirectory.swift`:

```swift
//
//  BeaconDirectory.swift
//  QuickRoom
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import Foundation

/// Maps a ranged beacon's (major, minor) to a backend workspace id via
/// GET /beacons. The cache persists in UserDefaults because region events
/// can relaunch the app in the background where a network round-trip may
/// not finish inside the wake window.
final class BeaconDirectory {
	static let shared = BeaconDirectory()

	private static let cacheDefaultsKey = "beaconDirectory.cache"

	private let client: APIClient
	private var cache: [String: String]

	init(client: APIClient = .shared) {
		self.client = client
		self.cache = UserDefaults.standard.dictionary(forKey: Self.cacheDefaultsKey) as? [String: String] ?? [:]
	}

	static func cacheKey(major: Int, minor: Int) -> String {
		"\(major)/\(minor)"
	}

	func workspaceId(major: Int, minor: Int) async -> String? {
		let key = Self.cacheKey(major: major, minor: minor)
		if let hit = cache[key] {
			return hit
		}
		await refresh()
		return cache[key]
	}

	func refresh() async {
		do {
			let response: BeaconsResponse = try await client.get("/beacons")
			cache = Dictionary(uniqueKeysWithValues: response.beacons.map { (Self.cacheKey(major: $0.major, minor: $0.minor), $0.workspaceId) })
			UserDefaults.standard.set(cache, forKey: Self.cacheDefaultsKey)
		} catch {
			print("BeaconDirectory: refresh failed: \(error)")
		}
	}
}
```

- [ ] **Step 4: Implement PresenceReporter**

`QuickRoom/Core/Network/PresenceReporter.swift`:

```swift
//
//  PresenceReporter.swift
//  QuickRoom
//
//  Created by Asadullokh Nurullaev on 05/07/26.
//

import Foundation
import UIKit

/// Sends arrive/leave events to POST /presence. Fire-and-forget: a lost
/// event is corrected by the backend's presence TTL backstop.
final class PresenceReporter {
	private static let lastWorkspaceKey = "presence.lastWorkspaceId"

	private let client: APIClient
	private let directory: BeaconDirectory

	init(client: APIClient = .shared, directory: BeaconDirectory = .shared) {
		self.client = client
		self.directory = directory
	}

	func reportEnter(major: Int, minor: Int) async {
		guard let workspaceId = await directory.workspaceId(major: major, minor: minor) else {
			print("PresenceReporter: no mapping for beacon \(major)/\(minor)")
			return
		}
		// Exit callbacks don't say which beacon, so remember where we are.
		UserDefaults.standard.set(workspaceId, forKey: Self.lastWorkspaceKey)
		await send(workspaceId: workspaceId, eventType: "entered")
	}

	func reportExit() async {
		guard let workspaceId = UserDefaults.standard.string(forKey: Self.lastWorkspaceKey) else { return }
		UserDefaults.standard.removeObject(forKey: Self.lastWorkspaceKey)
		await send(workspaceId: workspaceId, eventType: "exited")
	}

	static func identity() -> (userId: String, displayName: String) {
		if let user = AuthService.shared.currentUser {
			return (user.userId, user.name)
		}
		let deviceId = UIDevice.current.identifierForVendor?.uuidString ?? "unknown-device"
		return (deviceId, UIDevice.current.name)
	}

	private func send(workspaceId: String, eventType: String) async {
		let identity = Self.identity()
		let request = PresenceRequest(
			workspaceId: workspaceId,
			userId: identity.userId,
			displayName: identity.displayName,
			eventType: eventType,
			eventTs: Int64(Date().timeIntervalSince1970 * 1000)
		)
		do {
			let _: StatusResponse = try await client.post("/presence", body: request)
		} catch {
			print("PresenceReporter: \(eventType) for \(workspaceId) failed: \(error)")
		}
	}
}
```

- [ ] **Step 5: Rewire BeaconMonitoringService**

In `QuickRoom/Core/Services/BeaconMonitoringService.swift`:

(a) Remove, as Rei's `TODO: remove` markers direct: the `import UserNotifications` line, `UNUserNotificationCenterDelegate` conformance, the `UNUserNotificationCenter.current().delegate = self` line and its comment, the `userNotificationCenter(_:willPresent:…)` method, the whole `sendLocalNotification` helper, and every `sendLocalNotification(…)` call site.

(b) Add a reporter property after `private let targetUUID …`:

```swift
	private let presenceReporter = PresenceReporter()
```

(c) `didRange` becomes (replaces the whole method):

```swift
	func locationManager(_ manager: CLLocationManager, didRange beacons: [CLBeacon], satisfying beaconConstraint: CLBeaconIdentityConstraint) {
		guard isRanging, let closestBeacon = beacons.first else { return }

		isRanging = false
		manager.stopRangingBeacons(satisfying: beaconConstraint)

		let major = closestBeacon.major.intValue
		let minor = closestBeacon.minor.intValue
		Task {
			await presenceReporter.reportEnter(major: major, minor: minor)
		}
	}
```

(d) `didExitRegion` becomes (replaces the whole method, and its `// TODO: Implement server callback for exit and detect` marker — this is that implementation):

```swift
	func locationManager(_ manager: CLLocationManager, didExitRegion region: CLRegion) {
		guard region is CLBeaconRegion else { return }

		Task {
			await presenceReporter.reportExit()
		}
	}
```

(e) `didEnterRegion` keeps its ranging logic; only the `sendLocalNotification` line and its `// TODO: remove` comment go away.

- [ ] **Step 6: Prefetch the directory at app start**

In `QuickRoom/App/QuickRoomApp.swift`, extend `init()`:

```swift
	init() {
		_ = BeaconMonitoringService.shared
		Task {
			await BeaconDirectory.shared.refresh()
		}
	}
```

- [ ] **Step 7: Build + full test suite green** (build command, then test command)

- [ ] **Step 8: Commit**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git add QuickRoom/Core/Network/BeaconDirectory.swift QuickRoom/Core/Network/PresenceReporter.swift QuickRoom/Core/Services/BeaconMonitoringService.swift QuickRoom/App/QuickRoomApp.swift QuickRoomTests/PresenceTests.swift && git commit -m "Report beacon enter/exit to the backend presence API

Ranged (major, minor) resolves to a workspace via a cached /beacons
directory; exit uses the persisted last-known room since exit callbacks
carry no beacon identity. Replaces the placeholder debug notifications."
```

---

### Task 8: VPS APPLE_BUNDLE_ID + local compose mirror

Without this, `POST /auth/apple` rejects every token: the verifier compares the JWT `aud` against an EMPTY bundle id.

**Files:**
- Modify: `/root/roompulse/docker-compose.yml` + `/root/roompulse/.env` (VPS, over ssh `pr-diriger-hetzner`)
- Modify: `backend/docker-compose.yml` (this repo — ZoomIBeacon — mirror only)

**Interfaces:**
- Consumes: nothing from other tasks. Produces: live `/auth/apple` accepting `<app-bundle-id>` tokens (Rei's on-device runs, Task 9's PR checklist).

- [ ] **Step 1: Add the env passthrough on the VPS**

In `/root/roompulse/docker-compose.yml`, add to the `roompulse` service `environment:` block (after the `ZOOM_LOCATION_ID` line):

```yaml
      # Sign in with Apple: the iOS app bundle id the identity token's aud
      # must match. Empty = Apple sign-in rejected.
      APPLE_BUNDLE_ID: "${APPLE_BUNDLE_ID:-}"
```

And append to `/root/roompulse/.env`:

```
APPLE_BUNDLE_ID=<app-bundle-id>
```

- [ ] **Step 2: Recreate and verify**

```bash
ssh pr-diriger-hetzner 'cd /root/roompulse && docker compose -p backend up -d && sleep 2 && docker inspect roompulse --format "{{range .Config.Env}}{{println .}}{{end}}" | grep APPLE'
```

Expected: `APPLE_BUNDLE_ID=<app-bundle-id>`. Then confirm the endpoint still answers (bad token → clean 401, not 500):

```bash
curl -s -w "\n%{http_code}\n" -X POST https://rp.asadullokhn.uz/auth/apple -H 'Content-Type: application/json' -d '{"identity_token":"garbage"}'
```

Expected: `{"error":"invalid apple identity token"}` and `401`.

- [ ] **Step 3: Mirror in this repo + commit**

Apply the same `environment:` addition to `backend/docker-compose.yml` in ZoomIBeacon (its env block differs slightly — add after its Zoom vars), then:

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/ZoomIBeacon && git add backend/docker-compose.yml && git commit -m "Pass APPLE_BUNDLE_ID through compose for Sign in with Apple

The deployed verifier had an empty bundle id, so every mobile sign-in
would have been rejected with 401."
```

---

### Task 9: Live verification + push + PR

**Files:** none (verification + delivery)

- [ ] **Step 1: Full suite one last time** (test command → `** TEST SUCCEEDED **`)

- [ ] **Step 2: Live simulator run against production**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && DEVELOPER_DIR=/Applications/Xcode.app xcrun simctl boot "iPhone 17" 2>/dev/null; DEVELOPER_DIR=/Applications/Xcode.app xcodebuild -project QuickRoom.xcodeproj -scheme QuickRoom -destination 'platform=iOS Simulator,name=iPhone 17' -quiet build CODE_SIGNING_ALLOWED=NO && APP=$(find ~/Library/Developer/Xcode/DerivedData -path "*QuickRoom*/Build/Products/Debug-iphonesimulator/QuickRoom.app" | head -1) && DEVELOPER_DIR=/Applications/Xcode.app xcrun simctl install booted "$APP" && DEVELOPER_DIR=/Applications/Xcode.app xcrun simctl launch booted <app-bundle-id>
```

Then verify visually (screenshot: `xcrun simctl io booted screenshot /private/tmp/claude-501/-Users-asadullokhn-CascadeProjects-Personal-ZoomIBeacon/f977d8cd-e405-437b-89c7-dc2402730aaa/scratchpad/quickroom-sim.png`):
- Onboarding shows the Sign in with Apple button (don't gate on completing SIWA in the simulator — flaky).
- After skip + granting location "While Using": floorplan shows REAL room states — compare against `curl -s https://rp.asadullokhn.uz/reservations` (rooms with a current `booked` reservation render reserved).
- Tapping a room shows its real reservations list; unmapped/unbacked rooms render disabled.
- Reserve without sign-in → the sign-in sheet appears (proves the 401 path end-to-end against production).

- [ ] **Step 3: Push branch and open the PR**

```bash
cd /Users/asadullokhn/CascadeProjects/Personal/QuickRoom && git push -u origin feature/api-service
```

Open the PR with `gh pr create -R Reishandy/QuickRoom --base main --head feature/api-service`. Title: `Wire the app to the QuickRoom backend (API service, Sign in with Apple, beacon presence)`. Body must cover:
- What's wired: rooms/reservations from `rp.asadullokhn.uz`, booking + cancel with real conflicts (409), Sign in with Apple sessions, beacon enter/exit → presence (drives check-in/out + occupancy), 30 s auto-refresh. Swagger reference: `https://rp.asadullokhn.uz/docs`.
- Room mapping note: static polygons re-keyed to workspace ids in `StaticRooms.swift`, size-matched, `room-k` dropped — re-key freely.
- What Rei must do on his side: enable the **Sign in with Apple capability** for `<app-bundle-id>` in his paid team (entitlement file already in the diff); on-device checklist — sign in, book a room, walk into the beacon room (expect auto check-in on the admin panel at `https://rp.asadullokhn.uz/admin`), walk out (check-out).
- Known limits: presence heartbeat + notification polling are follow-ups; SIWA not verified in the simulator.
- NO Co-Authored-By; no emojis.

- [ ] **Step 4: Update the work log**

Append a row block to `/Users/asadullokhn/ObsidianVault/Default/Projects/Personal/RoomPulse/Challenge Work Log.md` under a `### 2026-07-05 — QuickRoom iOS API wiring (Rei's repo)` heading, listing: API layer + auth + reservations + presence (PR link), the xcconfig `//`-comment gotcha, the nanosecond-date gotcha, and the `APPLE_BUNDLE_ID` VPS fix.

---

## Self-Review Notes

- **Spec coverage:** xcconfig/entitlement (T1), test infra (T2, spec's "if clean" hedge encoded as fallback), APIClient+DTOs (T3), auth+Keychain (T4), rooms re-key + reservations + 30s refresh + disabled-unbacked (T5), onboarding/reserve UI + 401 prompt (T6), beacon presence + debug-notification removal + directory prefetch (T7), VPS APPLE_BUNDLE_ID + mirror (T8), live verify + PR + work log (T9). Heartbeat/notifications explicitly out of scope — none planned. ✓
- **Deviation from spec, deliberate:** spec said 401 "clears the session"; implemented as throw-only (`APIError.unauthorized` → UI prompts sign-in; stale Keychain token is overwritten on next sign-in). Simpler, no global state mutation inside the client.
- **Type consistency check:** `overlayServerRooms(onto:server:)` and `mapReservations(_:myUserId:)` match between T5 tests and implementation; `StatusResponse` used by T3/T4/T7; `BeaconDirectory.cacheKey` static used in T7 tests. `AuthService(client:)` init used in T4 tests matches signature. ✓
