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
`mobileEnabled`. Foreground delivery continues to use the organisation-scoped
NATS mobile subject, with REST polling as a compatibility fallback.

For background Android push, register the Android application in the same
Firebase project used by the server's FCM notification channel. The distributed
APK contains no deployment-specific `google-services.json`: it loads the public
Firebase options from the selected XACT server and initializes Firebase at
runtime. The app registers and refreshes its project-bound FCM token in the
signed-in user's notification options automatically. Push notifications open
the related device when tapped.

The Android package name registered in Firebase must be
`com.xact.iot.mobile`. Obtain `google-services.json` from Firebase **Project
settings > General > Your apps** and paste it into **Android
google-services.json** under **Notifications > Channels > Android Push (FCM)**.
Separately, generate a server service-account JSON key from **Project settings >
Service accounts > Firebase Admin SDK** and paste it into **Service Account
JSON**. The service-account document contains a private key and must never be
returned to the app or committed to source control.

The server exposes only the non-secret Android project ID, application ID, API
key, and Messaging Sender ID. Native Android startup persists and restores these
options so terminated-app notifications work. Switching to a server that uses a
different Firebase project triggers one automatic app restart.

$ flutter emulators --launch xact_api_36
$ flutter run -d emulator-5554
