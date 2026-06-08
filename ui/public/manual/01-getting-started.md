# Getting Started

Welcome to XACT - a real-time data acquisition, monitoring, and control platform. This guide will help you navigate the interface and get productive quickly.

## Logging In

Open your browser and navigate to the XACT URL provided by your administrator. You will be presented with a terminal-style login screen.

Enter your **login name** (or email address) and **password**, then click **Sign In**. The system accepts either your login name or your registered email address.

On successful authentication you will receive a session token and the main interface will load. Your session persists across browser refreshes until the token expires or you log out.

> If you have forgotten your password, contact your organisation administrator - they can reset it from the Users widget.

## The Main Interface

After logging in you will see the main application layout:

| Area | Location | Purpose |
|------|----------|---------|
| **Header** | Top | Page title, edit-mode toggle, clock, and user menu |
| **Sidebar** | Left edge | Navigate between dashboards, system dashboards, and switch organisations |
| **Content** | Centre | Displays the active dashboard or configuration view |

### Header Bar

The header bar contains:

- **Page title** - shows the name of the currently active dashboard
- **Edit toggle** - a pencil icon that switches the active dashboard into edit mode (if you have permission)
- **Clock** - a 24-hour real-time clock in the top-right area
- **User menu** - click your name to access Profile, Preferences, and Logout

### Sidebar

The sidebar is your primary navigation. It shows:

- **Organisation badge** - displays your current organisation name. Click to switch between organisations if you belong to more than one.
- **Dashboard list** - all dashboards and system dashboards you have permission to view, grouped into categories. Click a category header to expand or collapse it, and click a dashboard name to navigate to it.

On narrow screens (under 768px wide) the sidebar collapses automatically. Use the hamburger icon in the header to toggle it.

## Switching Dashboards

Click any dashboard name in the sidebar to switch views. The URL updates with a hash fragment (e.g. `#MyDashboard`) so you can bookmark specific dashboards or share links. On page reload, XACT returns you to your last active dashboard.

If you have unsaved changes on a dashboard, XACT will prompt you before navigating away.

## Switching Organisations

If your account belongs to multiple organisations, click the **organisation badge** at the top of the sidebar. A dropdown will appear listing all your organisations. Select one to switch - your session context changes and the sidebar reloads with that organisation's dashboards and data.

Each organisation has its own dashboards, tag tree, users, and configuration. Your role may differ between organisations.

## Themes

XACT includes several visual themes. To change your theme:

1. Click your **username** in the header bar and select **Preferences**.
2. Choose from the available themes - the change applies immediately across the entire interface.

Available themes include Dark Navy, Green Blue Chill, Lilac Elegance, and Sandstone. Your selection is saved in your browser and persists across sessions.

## Your Profile

Click your username in the header and select **Profile** to:

- View and edit your first name, last name, and email address
- Change your password
- Configure notification preferences (enable/disable email and Telegram notifications, set your Telegram ID)

## Logging Out

Click your **username** in the top-right corner of the header, then select **Logout**. You will be returned to the login screen and your session token will be cleared.

## Next Steps

- Prepare production deployments in [Preparing for Production](#preparing-production).
- Learn how organisations work in [Organisations](#organisations).
- Understand roles and permissions in [Users & Permissions](#users-permissions).
- Create and manage dashboards in [Dashboards](#dashboards).
- Explore the available widgets in [Widgets](#widgets).
- Understand real-time data tags in [Tags & Real-Time Data](#tags).
