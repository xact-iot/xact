# XACT Mobile

Flutter client for XACT operations. It uses the same JWT login, organisation
context, UI permissions, REST endpoints, report engine, and NATS WebSocket
interface as the web application.

## Run locally

Install an Android SDK, make it visible through `ANDROID_HOME`, then run:

```bash
flutter pub get
flutter run
```

The login screen accepts either a host name or URL. A host without a path is
normalised to `https://<host>/xact`; an explicit reverse-proxy path is kept.

## Android APK

```bash
flutter build apk --release
```

Before distributing a release, replace the temporary debug signing
configuration in `android/app/build.gradle.kts` with the deployment keystore.

To publish a self-hosted update through XACT, configure the server with:

```dotenv
MOBILE_APP_VERSION=1.0.1
MOBILE_APK_URL=/downloads/xact-mobile-1.0.1.apk
MOBILE_RELEASE_NOTES=Maintenance and reliability improvements
```

The APK URL may also be absolute. The update manifest itself is authenticated;
the APK host should enforce equivalent access when it is not served under the
authenticated XACT proxy.

## Notifications

Opt-in is stored in the signed-in user's existing `notificationOptions` as
`mobileEnabled`. The XACT notification dispatcher publishes matching profile
events to the user's organisation-scoped NATS mobile subject. The app displays
them through the native notification service and opens the related device when
tapped. REST event polling remains enabled as a compatibility fallback for
servers that predate the mobile channel.
