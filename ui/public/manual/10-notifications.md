# Notifications

Notifications deliver alerts to users when critical events occur. When an alarm is raised, a threshold is breached, or another significant event happens, XACT can automatically send messages through configured channels to the right people.

## How Notifications Work

The notification flow is:

1. An **event** is generated - typically by a processing block (limit check, flap filter) but potentially by any system component.
2. If the event has a **Notification ID**, XACT looks up the corresponding **notification profile**.
3. The profile specifies which **roles** and **users** should receive the alert.
4. A recipient list is assembled from the profile - all users with the specified roles plus individually named users (duplicates removed).
5. For each recipient, a message is sent through their enabled **channels** (email, Telegram, or both).
6. A delivery event is logged recording the recipients.

## Notification Channels

Channels are the delivery mechanisms for notifications. XACT currently supports two channels:

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

### Configuring Channels

1. Open the **Notifications** widget (System category).
2. Select the **Channels** tab.
3. Enter the settings for each channel you want to enable.
4. Save the configuration.

Channel settings are per-organisation - each organisation configures its own email server and Telegram bot.

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

Users can set these preferences in their **Profile** (accessible from the header user menu). Administrators can also set preferences via the **Users** widget.

A user will only receive notifications through channels they have enabled. If a user has email disabled, they will not receive email notifications even if they match a profile's role criteria.

## Permissions

Notification configuration requires the `notifications.manage` permission, granted to **Manager** and above by default.

All authenticated users can view their own notification preferences in their profile.
