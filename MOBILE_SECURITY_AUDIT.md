# Mobile security audit

Audit date: 2026-09-23. Source revision: `afe7fe4` (mobile's latest change: `f9198bd`). App version: `1.0.1+2`.

Reviewed the Flutter application, Android and iOS host code and configuration, authentication and account transitions, WebView integration, REST and NATS clients, Firebase notifications, report sharing, APK updates, release signing, and dependencies. Relevant server authentication, notification dispatch, and mobile release endpoints were traced to establish trust boundaries.

**Original result: seven findings: three High and four Medium.** All seven now have code fixes in the working tree, together with regression coverage and additional platform hardening. The findings and original evidence below describe revision `afe7fe4`, before remediation; their line references refer to that revision. Severity assumes an operational deployment with sensitive device and organisation data. Each finding identifies the conditions needed to exploit it; none is presented as an unconditional remote takeover.

## Remediation — 2026-09-24

| ID | Implemented protection |
| --- | --- |
| MOB-01 | HTTPS-only bounded APK streaming; the API bearer is restricted to the exact API origin at every redirect. |
| MOB-02 | Navigation allowlist, inert same-origin bootstrap, document URL check at handoff, and session storage for embedded web authentication. |
| MOB-03 | HTTPS/WSS enforcement in Dart, no automatic authenticated API redirects, and Android cleartext traffic disabled. |
| MOB-04 | Per-account preferences, random session-bound data-only push, persisted-session checks in foreground/background/tap handlers, and registration/token cleanup on logout. |
| MOB-05 | Central account teardown clears browser data and exports, invalidates old asynchronous work, and discards all navigation routes. |
| MOB-06 | Release builds require explicit production signing configuration and reject Android debug signing identities. |
| MOB-07 | Bounded NATS header/payload/buffer parsing, protocol-error disconnects, exact notification subjects, and session-aware connection disposal. |

Additional changes disable Android backup/transfer of application data, restrict
FileProvider access to the export cache, remove unused broad storage/media
permissions, enforce same-package/newer-version/current-signer APK checks, use
generated download filenames, and keep iOS secure session values on the same
device. Browser cleanup attempts every operation even when one fails and blocks
new authenticated content if cleanup cannot complete.

The server and UI changes are required integration work: deploy them before the
mobile app. Older servers cannot supply the secure dashboard bootstrap or
session-bound background-push protocol. Older clients retain their legacy push
behavior until upgraded. Production signing credentials must be supplied by the
deployment owner, and existing debug-signed installations need a migration.
See [mobile deployment instructions](mobile/README.md).

The diagnostics are now ordinary security regression tests in
[`mobile/test/security_test.dart`](mobile/test/security_test.dart); the old
audit command delegates to that suite. Validation results are recorded below.

### Remediation validation

- Flutter: 37 tests passed (25 security regressions plus the 12 existing tests);
  `flutter analyze --no-pub` reported no issues.
- Server: `go test ./notifications ./events ./rtdb/api` passed, including scoped
  FCM payloads, propagation of the session binding, public bootstrap routing at
  root/proxy prefixes, and OpenAPI route coverage.
- UI: all 10 tests in `test/utils-auth-registry.test.ts` passed; `npm run build`
  passed TypeScript and Vite compilation.
- Android: `:app:compileDebugKotlin` and `:app:processDebugMainManifest` passed.
  The merged manifest was checked for TLS policy, backup exclusions, the narrow
  non-exported FileProvider, removed media permissions, and disabled Firebase
  token auto-initialization.
- Release signing: `:app:validateProductionSigning` rejected the missing
  production keystore with the intended error. No production signed APK was
  generated; no existing private signing key was accessed.

The dependency versions are unchanged from the audit; two existing transitive
test packages are now declared directly as development dependencies. The OSV
inventory remains the dated audit result, not a new advisory scan. Physical
Android/iOS testing remains necessary for background/terminated push delivery,
WebView storage cleanup, external viewer grants, backup restore, and APK
installer behavior. iOS compilation was not available in this Linux environment.

## Original findings and evidence

| ID | Severity | Finding | Validation |
| --- | --- | --- | --- |
| MOB-01 | High | APK downloads disclose the API bearer token to another host | Two isolated HTTP-client reproductions |
| MOB-02 | High | WebView injects the session into an unchecked top-level page | Widget reproduction with a foreign page-finished event |
| MOB-03 | High | HTTP and WS can carry passwords and session credentials | HTTP-client reproduction and loopback WebSocket capture |
| MOB-04 | Medium | Logout and server changes retain the user's push registration | Session reproduction plus server/FCM source trace |
| MOB-05 | Medium | Logout leaves WebView credentials and downloaded reports behind | Widget/API reproductions plus storage and web bootstrap review |
| MOB-06 | Medium | Android release APK is signed with the debug identity | Build configuration and existing APK certificate verification |
| MOB-07 | Medium | Malformed NATS framing causes parser errors or stalls | Loopback broker reproduction and parser review |

### MOB-01 — API credentials sent to external APK hosts

**Location:** [api_client.dart:461](mobile/lib/services/api_client.dart#L461), [settings_screen.dart:224](mobile/lib/screens/settings_screen.dart#L224).

`downloadApk()` accepts an absolute `downloadUrl` and unconditionally supplies `_headers`, including `Authorization: Bearer <session>`. Neither the origin nor the scheme is checked. A legitimate deployment using a separate APK/CDN host therefore sends that host its users' full API tokens. An HTTP download URL exposes the same token on the network even when the API connection uses HTTPS.

**Exploit conditions:** The user accepts a mobile update, and the manifest points to an external host. Its operator, a compromise of that host, or an observer on an HTTP download path can obtain the token and replay it to the XACT API/NATS service with that user's privileges until expiry or server-side invalidation. The manifest itself is authenticated and configured by deployment environment variables; an ordinary unauthenticated user cannot simply edit it. The default same-origin APK route avoids the external-host case.

**Evidence:** Both diagnostic cases configure an HTTPS API, request an APK from a different host, and observe the complete synthetic bearer header. Both HTTPS and HTTP foreign URLs are accepted. No requests are sent to those example hosts; the HTTP client is mocked.

**Fix:** Require HTTPS. Send the API bearer only to the exact authorised API origin, checking scheme, hostname and effective port. External artifact downloads should use no API token, or a separate, narrowly scoped, expiring download credential. Validate redirect destinations as well as the initial URL.

### MOB-02 — WebView credential injection is not bound to the trusted origin

**Location:** [dashboard_screen.dart:78](mobile/lib/screens/dashboard_screen.dart#L78).

The dashboard loads the server root with unrestricted JavaScript. Its first `onPageFinished` callback ignores the URL and executes JavaScript containing the native JWT and user record. There is no `onNavigationRequest` policy. If that initial top-level load redirects to another origin, the token is injected into that origin's page context and local storage. Redirecting back immediately afterward does not undo disclosure: the destination page can intercept the storage write.

**Exploit conditions:** An attacker must control the first completed top-level page, for example through an interceptable HTTP connection, a compromised redirect destination, or an unsafe reverse-proxy/SSO redirect. No open redirect was established in the XACT server itself. An arbitrary iframe or a navigation after the one-time injection is not sufficient on its own.

**Evidence:** The widget reproduction loads the configured XACT root, invokes the real application callback for `https://attacker.example.test/landing`, and observes JavaScript containing the synthetic JWT. It also verifies that no navigation filter was installed. This proves the Dart application's decision; it is not a physical-device browser exploit test.

**Fix:** Restrict top-level navigation to explicitly trusted HTTPS origins and bind credential handoff to a trusted bootstrap document. Check the actual document origin inside the handoff script as well as in native navigation handling. Prefer a short-lived, single-use handoff over copying the full native session bearer into persistent JavaScript storage.

### MOB-03 — Cleartext credential transport is allowed in release code

**Location:** [api_client.dart:88](mobile/lib/services/api_client.dart#L88), [realtime_service.dart:51](mobile/lib/services/realtime_service.dart#L51), [AndroidManifest.xml:9](mobile/android/app/src/main/AndroidManifest.xml#L9).

Server URL validation explicitly accepts HTTP, and login posts the username and password to that URL. Android's release manifest allows cleartext traffic globally. The NATS client accepts a supplied `ws://` endpoint even when the API is HTTPS and sends the NATS password—currently the user's session JWT—in the CONNECT frame. The APK path has the additional downgrade exposure described in MOB-01.

**Exploit conditions:** The installation uses an explicit HTTP server, a WS broker endpoint, or an HTTP artifact URL. A network observer can steal credentials; an active network attacker can also alter API responses, alarms, dashboard content and update metadata. A bare hostname defaults to HTTPS, and no certificate-validation bypass was found for properly configured TLS connections.

**Evidence:** The login reproduction captures the synthetic password in an HTTP request. A loopback fake broker receives a CONNECT frame containing the synthetic session credential with `tls_required:false`, while the API is configured as HTTPS.

**Fix:** Enforce HTTPS/WSS in application URL validation, including server-supplied broker and release URLs. Disable release cleartext traffic, keeping any necessary development exceptions narrowly scoped to debug builds. Platform configuration alone is insufficient for Dart-owned sockets. [Flutter network-policy documentation](https://docs.flutter.dev/release/breaking-changes/network-policy-ios-android) and [Android cleartext guidance](https://developer.android.com/privacy-and-security/risks/cleartext-communications) describe this distinction and the exposure.

### MOB-04 — Push notifications outlive logout and account changes

**Location:** [session_controller.dart:101](mobile/lib/services/session_controller.dart#L101), [notification_service.dart:133](mobile/lib/services/notification_service.dart#L133), [notification_service.dart:183](mobile/lib/services/notification_service.dart#L183), [api_client.dart:433](mobile/lib/services/api_client.dart#L433), [home_shell.dart:49](mobile/lib/screens/home_shell.dart#L49).

Logout and changing servers delete the secure native session, but do not unregister the device's FCM token from the former user's server profile or delete/rotate the local Firebase token. `stop()` only changes a local flag and stops polling. The notification preference is device-global and survives logout, so another account can inherit an enabled preference and register the same device token while the former user's registration remains active.

[Server dispatch](server/events/handler.go#L355) uses the stored FCM registration without requiring an active mobile login. [The sender](server/notifications/fcm.go#L175) includes the event title/body in an FCM notification payload. Firebase handles background notification display independently of the Dart `_enabled` flag. [Firebase notification behavior](https://firebase.flutter.dev/docs/messaging/notifications/) documents that platform handling.

**Exploit conditions:** User A enables notifications, signs out or changes servers, and another person uses the device. Subsequent notifications for A can disclose organisation, device and event information while the app is signed out or logged in as B. This also matters when an account/session expires. It does not establish unauthenticated access to the full device API.

**Evidence:** A restored synthetic session logs out with zero server requests, while both the remote registration fixture and local enabled preference remain unchanged. The same is true after changing servers. Actual cloud delivery was not triggered; delivery after logout follows from the traced server payload and documented FCM behavior.

**Fix:** Associate registrations and notification preferences with server, project, user and device. Remove the matching old registration before discarding its credentials; handle offline logout explicitly, including local token deletion/rotation and safe re-registration. Clear displayed/pending notifications and discard callbacks from earlier sessions. Avoid disabling unrelated devices' notifications through a single global user preference.

### MOB-05 — WebView session data and reports survive logout

**Location:** [dashboard_screen.dart:96](mobile/lib/screens/dashboard_screen.dart#L96), [session_controller.dart:117](mobile/lib/services/session_controller.dart#L117), [api_client.dart:399](mobile/lib/services/api_client.dart#L399), [reports_screen.dart:44](mobile/lib/screens/reports_screen.dart#L44).

The JWT is copied from secure storage into persistent WebView local storage. Neither logout, server change nor dashboard disposal clears that storage, cookies or cached authenticated content. Reports are written to predictable, account-independent filenames in the temporary directory and are not deleted on logout. Report opening also does not recheck the active session after a pending download completes.

There is a concrete account-transition concern: [the web UI initializes authentication from existing local storage](ui/src/auth.ts#L166) during `DOMContentLoaded`, whereas mobile injects the new credentials only after page completion. The initial load of a later user's WebView can therefore begin using the previous user's still-valid token before replacement. If the next account has no dashboards, the retained WebView credential is not replaced at all.

**Exploit conditions:** Shared-device/account transitions, access to retained application data, or a download completing after sign-out. This is not a claim that every other Android app can read XACT's private cache. Extraction from a physical device and the exact stale-page timing were not tested.

**Evidence:** The widget reproduction injects a synthetic credential, logs out and disposes the dashboard; it observes no storage/cache/cookie clearing calls. The report reproduction verifies that a generated synthetic PDF remains after logout. Android's storage-clearing API is explicit, and Apple's default WebView data store persists website data. [Android WebStorage](https://developer.android.com/reference/android/webkit/WebStorage), [Apple WKWebsiteDataStore](https://developer.apple.com/documentation/webkit/wkwebsitedatastore).

**Fix:** Centralise session teardown. Stop/cancel work from the old session, clear WebView authentication and website data, delete sensitive temporary files, and complete cleanup before creating the next authenticated UI. Recheck session identity after asynchronous downloads before saving or opening their results. Use an isolated bootstrap so an old web session cannot run before handoff. Files intentionally exported to another app need a separate, clearly understood retention policy.

### MOB-06 — Release APK uses debug signing

**Location:** [build.gradle.kts:29](mobile/android/app/build.gradle.kts#L29).

The release build explicitly uses `signingConfigs.getByName("debug")`. Verification of the existing release APK confirms `C=US, O=Android, CN=Android Debug` as its signing identity. The README acknowledges this as temporary, but the build does not enforce replacing it.

**Exploit conditions and impact:** Someone who obtains the development debug keystore can sign a replacement accepted as an update to installations using that identity, subject to normal Android installation approval. Debug keys are development credentials rather than an isolated production signing authority. The key is not universally known merely because it is a debug key, and no private signing key was read or shown during this audit. Debug signing also does not mean the release manifest has `debuggable=true`; it does not.

**Evidence:** `apksigner verify --print-certs` successfully verified the cached release APK and reported the debug certificate. APK SHA-256: `3e384065b34ee5d658e926f8b89ec870d2348142e8c412e941f305fc5719cb5c`. Certificate SHA-256: `e1fd1016590bddd277974b3413ffb264c799f3ec633d6c2ec292503cd950ea18`. This is an existing local artifact, not a newly built or verified deployed APK.

**Fix:** Use a dedicated protected release signing key, keep it out of source control, and make release builds fail if signing configuration is missing. Separate development and production application identities. Plan migration for any installations already distributed under the debug certificate. [Android signing guidance](https://developer.android.com/studio/publish/app-signing).

### MOB-07 — NATS frame parsing accepts invalid lengths and unbounded buffering

**Location:** [realtime_service.dart:104](mobile/lib/services/realtime_service.dart#L104).

The handwritten parser does not reject negative MSG lengths, bound header/payload/buffer sizes, or recover from nonnumeric lengths. A negative length reaches `sublist()` and raises a `RangeError`. A nonnumeric length leaves the same header at the head of the buffer, preventing subsequent messages from being processed. A declared large payload or a stream without CRLF can keep accumulating bytes. `_connected` can remain true, so notification polling may remain suppressed while the stream is unusable.

**Exploit conditions:** A malicious/compromised NATS WebSocket endpoint, or an active attacker on an allowed WS connection, must control protocol framing. An ordinary publisher cannot forge NATS framing just by publishing a malformed JSON value through a conforming broker. Availability impact includes lost updates/notifications and unhandled errors; exhaustion is a source-established risk, not a deliberately triggered out-of-memory test.

**Evidence:** The loopback broker sends a small negative-length MSG frame. The reproduction captures a `RangeError` and confirms `connected` remains true. It does not crash a production app or flood a server.

**Fix:** Use a maintained NATS client or implement bounded parsing with nonnegative length checks, maximum header/payload/buffer limits, protocol-error disconnects and reliable connection-state changes. Catch parser failures, release buffered data, and resume the REST fallback. Also bind pending connection work to the current session so disposal cannot be followed by a late connection completing.

## Additional hardening and validation items

These are distinct from the seven findings above; no additional working exploit is claimed.

- **Backup policy:** The merged Android release manifest has no explicit `allowBackup`, `fullBackupContent` or `dataExtractionRules`. Default backup participation deserves explicit review because the app persists identity and WebView data. Exclude credentials and authenticated website state from both cloud backup and device transfer, and test restore on supported Android versions. Do not assume encrypted secure-storage values restore successfully without their original keystore keys. No backup extraction was attempted. [Android backup behavior](https://developer.android.com/identity/data/autobackup).
- **File sharing and permissions:** `open_filex` contributes a FileProvider mapping for the filesystem root and broad files/cache/external directories, plus media/storage permissions. The provider is **not exported**, and current calls grant read access to the selected file; no arbitrary-file disclosure path was established. Override it with narrow export directories and remove permissions not needed for PDF/APK sharing. [Android FileProvider guidance](https://developer.android.com/privacy-and-security/risks/file-providers).
- **Update verification and resource bounds:** APK responses are fully buffered, have no download-specific timeout/size limit, and are offered to the installer without app-level checks of package identity, signing certificate or expected digest. Android still checks signatures when replacing an existing package and requires installation approval; the audit does not claim silent arbitrary-code installation. Validate the expected package/signer, use a trusted manifest, enforce bounded streaming and a timeout, and use a safe generated cache filename. The unsanitised version string is not reported as demonstrated arbitrary file overwrite: the prefixed directory component prevents a simple `../` payload from working in a fresh cache.
- **Notification and lifecycle races:** A poll already awaiting a response can display an event after `stop()`, and Firebase callbacks are not bound to a particular server/user generation. Cancel or reject stale asynchronous results. Review lock-screen notification content and app-switcher snapshots according to the deployment's data sensitivity.
- **URL grammar and platform validation:** Reject server URLs with embedded credentials, query strings or fragments. `Uri.replace(query: null, fragment: null)` retains those existing URI components rather than clearing them. Check release TLS behavior and account cleanup on real Android/iOS devices; do not treat missing certificate pinning, obfuscation or root detection alone as proven vulnerabilities.

## Dependency and native-platform results

- Flutter `3.44.6`, Dart `3.12.2`; 91 hosted Pub packages from `pubspec.lock`, including development dependencies.
- Resolved the current Android `releaseRuntimeClasspath` using the existing Gradle cache, then queried 116 public Maven coordinates and the 91 Pub packages against OSV. **No matching advisories in those 207 version queries.** [Machine-readable query inventory/results](mobile/security_audit/dependency-audit.json); [OSV query API](https://google.github.io/osv.dev/post-v1-querybatch/).
- An initial scan of compile and package metadata flagged `play-services-basement:16.0.1`. Current Gradle resolution upgrades the packaged version to `18.9.0`, so that compile-only hit is excluded. The advisory was fixed in `18.0.2`. [GHSA-cm6r-892j-jv2g](https://osv.dev/vulnerability/GHSA-cm6r-892j-jv2g).
- Existing Android release manifest: minimum SDK 24, target SDK 36, no `debuggable=true`. Exported Firebase receivers require Google's sender permission; the profile installer receiver requires `DUMP`. The app's exported activity is its normal launcher; no custom deep-link or arbitrary native command interface was found.
- Native secure storage uses the plugin's Android authenticated-encryption/keystore defaults and iOS Keychain defaults. No production private keys, hardcoded passwords, certificate-validation bypass, or general WebView-to-native JavaScript bridge was found in the reviewed mobile source. Public Firebase project configuration is not a private service-account credential.
- Native lists, device values and events render through Flutter text widgets. The WebView handoff uses JSON encoding for literals, so the issue is where credentials are injected, not missing JavaScript string escaping. Previously fixed server/UI findings were not counted again.
- iOS host configuration has no broad ATS exception. The repository has no resolved iOS native dependency lockfile or IPA available for inspection. Flutter engine, OS/WebView vulnerabilities and device patch levels are outside the package-query result; no-advisory results are not a guarantee of safety.

## Original audit validation and limits

The 12 existing mobile tests passed. Eight additional isolated diagnostic tests passed, reproducing the unsafe behaviors described above. Final `flutter analyze --no-pub` reported no issues. Gradle resolved the Android release dependency graph successfully in offline mode. The existing APK signature was verified; no new APK was installed or distributed.

Reproduce the diagnostic checks from `mobile/`:

```sh
flutter test --no-pub --reporter expanded security_audit/mobile_security_audit_test.dart
```

At audit time, the diagnostics deliberately passed when the vulnerable behavior existed. They have since been converted into ordinary regression tests that assert the protected behavior; the command above runs those updated tests. The current suite uses synthetic sessions, mocked HTTP/storage/WebView/notification interfaces, temporary files, and an in-memory fake WebSocket channel. [Regression source](mobile/test/security_test.dart).

This is a comprehensive source/configuration review with targeted local reproductions, not a claim that all possible exploits have been ruled out. No live accounts, production endpoints, private signing keys, cloud push delivery, physical-device storage, jailbreak/root environment or installed mobile app data were accessed. Physical-device WebView redirect/persistence behavior, backup restore, notification delivery after logout, external viewer grants and iOS release behavior remain acceptance tests for remediation.
