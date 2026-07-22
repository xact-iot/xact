# XACT Mobile App

XACT Mobile provides phone-friendly access to devices, events, dashboards, reports, and notifications. It uses the same XACT account, organisation, permissions, and server data as the web application.

> XACT Mobile currently supports **Android only**. An iPhone/iPad version is not currently available, and the app is not distributed through the Google Play Store.

## Installing the App

Install XACT Mobile directly from your organisation's XACT server:

1. On the Android phone, open the XACT web application in a browser and sign in.
2. Open the **hamburger menu** in the page header.
3. Select **Download Android App**. This menu entry is shown only when the web application is opened on an Android device.
4. Accept the browser's download warning if one is displayed.
5. Open the downloaded `xact-mobile.apk` file from the browser or the phone's Downloads app.
6. If Android blocks the installation, open the offered **Install unknown apps** setting and allow installations from the browser or Downloads app being used. Return to the installer afterward.
7. Tap **Install**, then tap **Open** when installation completes.

Only install an APK downloaded from your organisation's trusted XACT server. The wording and location of Android installation prompts can vary by phone manufacturer and Android version.

The app can subsequently check the same server for updates. In XACT Mobile, open **Settings → Check for mobile update** and follow the installation prompt. An update installs over the existing app and retains its saved settings.

## First Login

The first login screen asks for three values:

| Field | What to enter |
|-------|---------------|
| **XACT server** | The address of the XACT web application supplied by your administrator, for example `https://xact.example.com/xact`. If only a host name is entered, the app assumes HTTPS and the standard `/xact` path. |
| **Username or email** | The same login name or email address used for the XACT web application. |
| **Password** | The password for that XACT account. |

Tap **Sign in**. The available organisations, devices, dashboards, events, and reports are determined by the account's existing permissions.

The app remembers the server and securely stores the session on the phone. When the session expires, sign in again with the same details. Contact an administrator if the server address or credentials are not known.

## Enabling Notifications

After signing in:

1. Select **Settings** in the bottom navigation bar.
2. Under **Notifications**, enable **Server notifications**.
3. Allow notifications when Android displays its permission prompt.

Enabling the switch registers this Android installation with the XACT server and enables Android push for the signed-in user. The user must also be included in the relevant notification profile, directly or through a role. Tapping a notification opens XACT Mobile and, when a device is identified, opens that device.

If notifications do not arrive, confirm that notifications are permitted for XACT Mobile in Android settings and contact an administrator to check the server's FCM channel and notification profiles. See [Notifications](#notifications) for the complete notification configuration and troubleshooting guide.

## Using the App

The bottom navigation bar provides five areas:

- **Devices** — browse configured device groups, search for a device, and open its overview. A device overview shows available KPI values and recent events for that device. Pull down to refresh.
- **Events** — review recent events and filter them by severity. Tap an event associated with a device to open that device.
- **Dashboards** — open dashboards permitted for the account. The administrator can choose the dashboard initially shown by the app.
- **Reports** — view the available PDF report templates and tap a report to download and open it.
- **Settings** — view the signed-in user, change the server, switch organisation when permitted, enable notifications, check for app updates, or sign out. Changing the server saves the new address and signs you out so you can authenticate with an account on that server.

Live device values and notifications update while the app is connected. The app only displays data and actions allowed by the signed-in user's XACT permissions.

## Administration

### Mobile App Configuration Widget

Administrators configure the app with the **Mobile App Configuration** widget, available in the **System** widget category. Its settings are stored separately for each organisation.

The widget controls:

- **Device parent nodes** — each configured parent node becomes a device-type tab in the app. The app discovers devices beneath those nodes. If no parent nodes are configured, it searches all device nodes available to the organisation.
- **Default dashboard** — selects the dashboard initially displayed in the app. If it is unavailable to a user, the app falls back to the first permitted dashboard.
- **Organisation** — System Administrators can switch organisation in the widget before editing its mobile configuration.

Users require `mobile-app.read` to view this configuration and `mobile-app.write` to change it. Saved changes take effect when the mobile app refreshes its data.

### Android Push Configuration

Android push notifications use Firebase Cloud Messaging (FCM). The server requires the Android Firebase configuration and a server-side service-account credential, and users must be assigned to appropriate notification profiles. See the **Android Push (FCM)** section in [Notifications](#notifications) for setup, device registration, testing, security guidance, and troubleshooting.
