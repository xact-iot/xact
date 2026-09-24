# Mobile audit evidence

See [MOBILE_SECURITY_AUDIT.md](../../MOBILE_SECURITY_AUDIT.md) for scope, prerequisites, findings and limitations.

Run from `mobile/`:

```sh
flutter test --no-pub --reporter expanded security_audit/mobile_security_audit_test.dart
```

The original exploit reproductions have been replaced with security regression
tests in [`test/security_test.dart`](../test/security_test.dart). They now pass
only when the protected behavior is present and run in the normal `flutter test`
suite. The command above remains a wrapper for the same tests.

The tests use synthetic credentials, mocked HTTP/storage/WebView/notification
interfaces, disposable temporary files, and an in-memory WebSocket channel.
They do not contact a real XACT/FCM server or launch an installer. Flutter's SDK
cache must be available to the test runner.

`dependency-audit.json` records the 2026-09-23 OSV results for locked Pub packages and the resolved Android release runtime graph. It also records an excluded compile-only advisory hit. Native iOS packages, Flutter engine vulnerabilities and device WebView versions are not covered by those queries.
