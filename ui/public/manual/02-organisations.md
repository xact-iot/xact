# Organisations

XACT is a multi-tenant platform. Each **organisation** is an independent workspace with its own dashboards, tag tree, users, permissions, and configuration. A single XACT instance can serve multiple organisations simultaneously, with complete data isolation between them.

## How Organisations Work

Every piece of data in XACT is scoped to an organisation:

- **Tag tree** - each organisation has its own device hierarchy and tag data
- **Dashboards** - dashboards and widget layouts are per-organisation
- **Users & roles** - users can belong to one or more organisations with different roles in each
- **Permissions** - role-based access control is configured independently per organisation
- **Reports** - PDF report templates are organisation-specific
- **Notifications** - notification profiles and channel configuration are per-organisation
- **API keys** - each organisation manages its own API keys for programmatic access

## Switching Organisations

If your account is a member of multiple organisations:

1. Click the **organisation badge** at the top of the sidebar - it shows your current organisation name.
2. A dropdown appears listing all organisations you belong to.
3. Select the organisation you want to switch to.

The interface reloads with the selected organisation's context. Your role and permissions may be different in each organisation.

## Managing Organisations

Organisation management is controlled by the `organisations.view` and `organisations.change` permissions. Users with `organisations.view` can open the **Organisations** widget in read-only mode. Users with `organisations.change` can create, edit, delete, and manage API keys.

### Creating an Organisation

1. Click **New Organisation**.
2. Enter the organisation **name** (used internally as an identifier) and **display name**.
3. Optionally set the **geographic bounds** using the interactive map - click and drag to define the area. These bounds are used by the Map widget to set the default view.
4. Click **Save**.

### Editing an Organisation

Select an organisation from the list to edit its:

- **Display name** - the human-readable name shown in the interface
- **Active status** - deactivate an organisation to temporarily disable access
- **Geographic bounds** - the default map area for this organisation

### Deleting an Organisation

Select an organisation and click **Delete**. This permanently removes the organisation and all associated data. This action cannot be undone.

## API Keys

Each organisation can create **API keys** for programmatic access. API keys are used by external devices and integrations to send data into XACT via the REST ingest API.

### Managing API Keys

From the Organisations widget, select an organisation and navigate to the **API Keys** section:

- **Create** - click **New API Key**, enter a descriptive name, and click Generate. The key is displayed once - copy it immediately as it cannot be retrieved later.
- **List** - view all active API keys with their names and creation dates.
- **Delete** - revoke an API key by clicking the delete button next to it. Any devices using this key will immediately lose access.

Each organisation can have up to 5 active API keys. Keys are used in the `Authorization: ApiKey <key>` header when calling the ingest API.

> **Security note:** Treat API keys like passwords. Do not share them in plain text or commit them to version control.
