# Notifications

Notifications deliver alerts to users when critical events occur. When an alarm is raised, a threshold is breached, or another significant event happens, XACT can automatically send messages through configured channels to the right people.

## How Notifications Work

The notification flow is:

1. An **event** is generated - typically by a processing block (limit check, flap filter) but potentially by any system component.
2. If the event has a **Notification ID**, XACT looks up the corresponding **notification profile**.
3. The profile specifies which **roles** and **users** should receive the alert.
4. A recipient list is assembled from the profile - all users with the specified roles plus individually named users (duplicates removed).
5. For each recipient, a message is sent through their enabled **channels** (email, Telegram, Android push, or any combination).
6. A delivery event is logged recording the recipients.

## Notification Channels

Channels are the delivery mechanisms for notifications. XACT supports email, Telegram, and Firebase Cloud Messaging (FCM).

### Email

Sends notifications via SMTP. Configure the email channel with:

| Setting | Description |
|---------|-------------|
| SMTP Host | Mail server address |
| SMTP Port | Mail server port (typically 587 for TLS) |
| Username | SMTP authentication username |
| Password | SMTP authentication password |
| From Address | The sender email address |

### Telegram

Sends notifications via a Telegram bot. Configure with:

| Setting | Description |
|---------|-------------|
| Bot Token | The Telegram Bot API token (obtained from @BotFather) |

Each user who wants to receive Telegram notifications must enter their **Telegram ID** in their user profile. Administrators can also set this via the Users widget.

### Android Push (FCM)

Sends background push notifications to the XACT Android app through the Firebase Cloud Messaging (FCM) HTTP v1 API. FCM requires configuration on both sides:

- The **XACT server** needs a service-account JSON key so it can authenticate when sending messages.
- The **XACT server** also holds the Android app's `google-services.json` and publishes only its non-secret identifiers to binary Android clients at runtime.

These are different files. The service-account file contains a private key and must stay on the server. Never place it in the Android app, APK, a Git repository, email, or chat. The Android `google-services.json` file contains application identifiers rather than the server's private credential. XACT parses it and returns only the project ID, application ID, API key, and Messaging Sender ID to the app.

See the official Firebase guides for [FCM HTTP v1 authorization](https://firebase.google.com/docs/cloud-messaging/send/v1-api), [adding Firebase to Android](https://firebase.google.com/docs/android/setup), and [FCM for Flutter](https://firebase.google.com/docs/cloud-messaging/flutter/get-started).

#### 1. Create or select a Firebase project

1. Open the [Firebase console](https://console.firebase.google.com/).
2. Create a project, or select the project that will deliver XACT notifications.
3. Open **Project settings → General** and record the **Project ID**.
4. Open **Project settings → Cloud Messaging** and confirm that the **Firebase Cloud Messaging API (V1)** is enabled. If it is disabled, follow the link to the Google Cloud API console and enable it.

Use the same Firebase project for the server credential and Android app registration. A project mismatch is a common cause of rejected registration tokens.

#### 2. Obtain the server service-account key

The simplest setup uses the Firebase Admin service account:

1. In the Firebase console, open **Project settings → Service accounts**.
2. Select **Firebase Admin SDK**.
3. Click **Generate new private key**.
4. Confirm **Generate key**. Your browser downloads a JSON file.
5. Store the downloaded file securely. Firebase cannot show the private key again after creation.

For a least-privilege custom service account, create one under **Google Cloud console → IAM & Admin → Service Accounts**, grant it the **Firebase Cloud Messaging API Admin** role, then open **Keys → Add key → Create new key → JSON**. An administrator may need the Service Account Key Admin role. Some Google Cloud organisations disable downloadable service-account keys by policy; ask the organisation administrator rather than weakening the policy without review. See Google's [service-account key guidance](https://cloud.google.com/iam/docs/keys-create-delete).

The JSON pasted into XACT must contain at least:

```json
{
  "type": "service_account",
  "project_id": "your-firebase-project-id",
  "client_email": "service-account@your-firebase-project-id.iam.gserviceaccount.com",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n"
}
```

Do not edit the private key or replace its `\n` sequences. Paste the complete downloaded JSON document.

#### 3. Register the XACT Android app

1. In the same Firebase project, open **Project settings → General**.
2. Under **Your apps**, click **Add app** and select **Android**.
3. Enter this Android package name exactly:

   ```text
   com.xact.iot.mobile
   ```

4. Enter an optional nickname. A SHA certificate fingerprint is not required for FCM push notifications.
5. Click **Register app** and download `google-services.json`.

The standard XACT binary does not contain a deployment-specific Firebase file and does not need to be rebuilt. It obtains these public settings from the selected XACT server before signing in.

#### 4. Configure the XACT server

1. Sign in to XACT with `notifications.manage` permission.
2. Open the **Notifications** widget and select **Channels**.
3. Find **Android Push (FCM)**.
4. Paste the complete server service-account JSON into **Service Account JSON**.
5. Paste the complete Android configuration downloaded in the previous section into **Android google-services.json**.
6. Click **Save Channels**.

The Android Firebase project is server-wide because one running Android app process uses one Firebase Messaging project for the selected XACT server. Configure all organisation-specific FCM sender credentials against that same Firebase project. Rotating a service-account key requires pasting the new JSON into XACT and saving the channels again; after confirming delivery, delete the old key in Google Cloud IAM.

#### 5. Register an Android user and device

1. Install the standard XACT APK on an Android device with Google Play services, or an emulator image that includes Google APIs/Google Play.
2. Open the app at least once and sign in. FCM cannot register an installation until the app has run.
3. Open **Settings** in the XACT app and enable **Server notifications**.
4. On Android 13 or newer, allow notifications when Android asks for permission.

Before signing in, the binary app downloads the selected server's public Firebase options and initializes Firebase dynamically. It then obtains an FCM registration token and stores the token and Firebase project ID in the signed-in user's notification options. It also updates the token automatically when Firebase rotates it. In the web UI, **Profile Settings → Notifications** should show **Android push** enabled and an **FCM Registration Token**. Administrators can inspect or update the same fields in the **Users** widget.

When a user changes to an XACT server backed by a different Firebase project, the app saves the new public options and restarts itself once. On later cold starts, native Android startup initializes the stored project before Firebase handles background or terminated-app delivery.

An FCM registration token identifies an app installation, not a person and not the Firebase service account. Treat it as operationally sensitive. Reinstalling the app, clearing app data, restoring onto another device, or Firebase rotation can change it.

XACT currently stores one FCM registration token per user. Signing the same user into another Android installation replaces the previous token; use separate XACT users when multiple devices must receive the same profile notification.

#### 6. Test the setup

Test the Android side first, then the complete XACT path:

1. Copy the user's FCM registration token from their XACT profile.
2. In the Firebase console, open **Messaging**, create a notification, choose **Send test message**, paste the registration token, and send it while the app is in the background. This confirms the Firebase project, runtime Android configuration, device services, and token.
3. In XACT, confirm that the user has **Android push** enabled and belongs to a notification profile either directly or through a role.
4. Trigger an XACT event whose **Notification ID** references that profile.
5. Confirm that the Android notification arrives. Tapping an XACT FCM notification opens the app and selects the related device when the event includes one.

The Firebase console test does not test the XACT server credential. The final profile/event test does.

#### Troubleshooting Android push

| Symptom | Checks |
|---------|--------|
| Firebase is not configured in the app | Confirm both JSON documents are saved in **Notifications → Channels → Android Push (FCM)** and that `google-services.json` contains `com.xact.iot.mobile`. No APK rebuild is required. |
| No token appears in the profile | Open the installed app, sign in, enable Server notifications, verify network access to Google services, and use a device/emulator with Google Play services. |
| Console reports `SERVICE_VERSION_UPDATE_REQUIRED` or “Google Play services out of date” | Update Google Play services through the Play Store. For an emulator, use or recreate it with a current **Google Play** system image and update Play services inside the emulator. XACT remains usable, but FCM push is unavailable until the update is complete. |
| Notifications do not appear on Android 13+ | Enable notification permission in Android system settings and in the XACT app. |
| Firebase console test fails | Confirm the token came from an APK registered in the same Firebase project. Reopen the app to refresh the token. |
| XACT delivery fails but Firebase console test works | Verify the FCM HTTP v1 API is enabled, the server JSON belongs to the same project, and its service account has the Firebase Cloud Messaging API Admin role. |
| Delivery stopped after reinstall or clearing app data | Open XACT and enable notifications again so the new installation token is registered. |
| App was force-stopped in Android settings | Open the app again. Android does not resume FCM delivery to a force-stopped app until it is relaunched. |
| Key creation is unavailable | A Google Cloud organisation policy may prohibit user-managed keys. Contact the organisation administrator. |

If a service-account JSON key may have been exposed, disable or delete that key immediately in **Google Cloud console → IAM & Admin → Service Accounts → Keys**, generate a replacement, and update the XACT channel configuration.

### Configuring Channels

1. Open the **Notifications** widget (System category).
2. Select the **Channels** tab.
3. Enter the settings for each channel you want to enable.
4. Save the configuration.

Email and Telegram channel settings are per-organisation. FCM sender credentials may be configured per organisation, but every organisation on one server must use the same server-wide Android Firebase project.

## Notification Profiles

A notification profile defines **who** receives alerts and is referenced by events to determine delivery targets.

### Creating a Profile

1. Open the **Notifications** widget and select the **Profiles** tab.
2. Click **New Profile**.
3. Configure the profile:

| Field | Description |
|-------|-------------|
| **Name** | A short identifier (e.g. "SysAdmin", "OperationsTeam") |
| **Description** | What kind of events this profile covers (e.g. "Server issues", "Operational alerts") |
| **Roles** | Zero or more roles - all users with these roles will receive notifications |
| **Users** | Zero or more individual users to notify |
| **Acknowledge Required** | Whether recipients must acknowledge the notification (future feature) |

4. Save the profile.

### Predefined Profiles

XACT creates these default notification profiles:

| Profile | Description | Target Roles |
|---------|-------------|-------------|
| **SysAdmin** | Server issues | SystemAdmin |
| **Manager** | Operational issues | Manager |
| **Technician** | Technical issues | Technician |

You can modify these profiles or create additional ones as needed.

### Using Profiles with Processing Blocks

To send notifications when an alarm condition occurs:

1. Open the **Tags Manager** widget and navigate to the tag.
2. Edit the tag's pipeline and configure the **Limit Check** block.
3. Enable the event for the alarm condition (high limit, low limit, or return to normal).
4. Set the event **severity** and **message**.
5. Assign the **Notification ID** - this links the event to a notification profile.

When the alarm triggers, an event is created with the notification profile reference, and alerts are sent to all recipients defined in that profile.

## User Notification Preferences

Each user controls which channels they receive notifications on:

- **Email notifications** - enable/disable (the user's email address is used)
- **Telegram notifications** - enable/disable (requires a Telegram ID)
- **Android push** - enable/disable (requires an FCM registration token; the XACT Android app manages this automatically)

Users can set these preferences in their **Profile** (accessible from the header user menu). Administrators can also set preferences via the **Users** widget.

A user will only receive notifications through channels they have enabled. If a user has email disabled, they will not receive email notifications even if they match a profile's role criteria.

## Permissions

Notification configuration requires the `notifications.manage` permission, granted to **Manager** and above by default.

All authenticated users can view their own notification preferences in their profile.
