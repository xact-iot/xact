# Users & Permissions

XACT uses a role-based access control system to manage what users can see and do within each organisation.

## Roles

The following roles are available, listed from most to least privileged:

| Role | Description |
|------|-------------|
| **SystemAdmin** | Full system access across all organisations. Can manage organisations, users, and all settings. |
| **Admin** | Full access within their organisation. Can manage users, permissions, and all org resources. |
| **Manager** | Can manage most resources - configure reports, notifications, permissions, and tag calcs. |
| **Technician** | Can configure devices, tags, and processing pipelines. |
| **Operator** | Can interact with dashboards, view data, and acknowledge events. |
| **User** | Read-only access to dashboards and data. |

Each user is assigned one or more roles within their organisation. The most privileged role determines their effective access level. A user can have different roles in different organisations.

**SystemAdmin** is a special role that bypasses all permission checks - users with this role have unrestricted access to everything in the system.

## Managing Users

Administrators can manage users through the **Users** widget (found under System widgets). This widget provides a table of all users showing their name, email, login name, roles, last login time, and active status.

### Creating a User

1. Click **New User**.
2. Fill in the required fields:
   - **First Name** and **Last Name**
   - **Login Name** - the username they will sign in with
   - **Email** - must be unique across the system
   - **Password** - initial password (the user can change it later)
3. Assign one or more **roles** for the user within the current organisation.
4. Click **Save**.

### Editing a User

Select a user from the table to edit their:

- **Name and email** - update personal details
- **Active status** - deactivate an account to prevent login without deleting it
- **Organisation membership** - assign the user to organisations
- **Roles** - change role assignments per organisation
- **Notification preferences** - enable/disable email and Telegram notifications, set Telegram ID

### Resetting a Password

Select a user and click **Reset Password**. A new temporary password is generated and displayed inline - share it with the user securely. They should change it after their next login via Profile.

### Deactivating a User

Toggle the **Active** status to deactivate an account. Deactivated users cannot log in but their data and history are preserved. Reactivate at any time by toggling the status back.

## Permissions

Permissions control fine-grained access to features and resources. Each permission is a **resource.action** pair (e.g. `dashboards-setup.edit`, `reports.manage`) that can be granted or denied per role.

### Viewing Permissions

The **Permissions** widget (System category) displays the full permission matrix. Each row is a resource, each column is a role, and each cell shows whether that role has access.

### Permission Keys

| Key | Controls | Default Roles |
|-----|----------|--------------|
| `nodes.read` | View the tag tree structure | All roles |
| `nodes.write` | Create, edit, delete nodes | Technician and above |
| `tags.read` | View tag values and metadata | All roles |
| `tags.write` | Modify tag values and configuration | Technician and above |
| `dashboards-setup.read` | View dashboard configuration | Technician and above |
| `dashboards-setup.edit` | Create, edit, delete dashboards | Technician and above |
| `users.manage` | Create and edit user accounts | Admin and above |
| `organisations.view` | View organisations in read-only mode | As assigned |
| `organisations.change` | Create, edit, delete, and manage organisation API keys | SystemAdmin by default |
| `permissions.manage` | Modify role permissions | Admin and above |
| `reports.manage` | Create and manage PDF report templates | Manager and above |
| `notifications.manage` | Configure notification channels and profiles | Manager and above |
| `tagcalcs.manage` | Create and manage tag calculations | Manager and above |
| `logs.read` | View system event logs | Operator and above |

### Editing Permissions

With the `permissions.manage` permission, you can modify which roles have access to which resources:

1. Open the **Permissions** widget.
2. Find the resource you want to modify.
3. Toggle access on or off for each role.
4. Changes save automatically.

> **Caution:** Be careful when modifying permissions for your own role - you could lock yourself out of the permissions editor.

## How Permissions Are Enforced

Permissions are enforced at two levels:

- **UI level** - menu items, widgets, and features are hidden or disabled if the user lacks the required permission. Widgets display an "Insufficient permissions" message when access is denied.
- **Server level** - API endpoints verify permissions on every request. Even if the UI were bypassed, the server would reject unauthorised operations.
