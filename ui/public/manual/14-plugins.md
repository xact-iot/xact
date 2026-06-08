# Plugins

XACT supports a plugin system for extending functionality without modifying the core codebase. Plugins are loaded at runtime from the file system.

## Plugin Directory Structure

Plugins are stored in a directory tree at the root of the XACT installation:

```
plugins/
├── authentication/     Server-side authentication plugins
├── widgets/            Custom dashboard widgets (JavaScript)
├── map widgets/        Custom map item types (future)
└── themes/             Custom visual themes (future)
```

This directory structure is created automatically on server startup if it does not exist.

## Widget Plugins

Widget plugins are the most common extension point. They allow you to create custom dashboard widgets that integrate fully with the XACT UI - including real-time data subscriptions, configuration persistence, and permissions.

### How Widget Plugins Work

1. Place a `.js` file in the `plugins/widgets/` directory on the server.
2. On startup, XACT discovers all plugin files and makes them available via the REST API.
3. The UI loads plugin scripts dynamically at runtime.
4. Plugin widgets appear under the **Custom** category in the dashboard widget toolbar.
5. Users can drag them onto dashboards and configure them like any built-in widget.

### Creating a Widget Plugin

A widget plugin is a JavaScript file that defines a web component (custom element) and registers it with the XACT widget system.

#### Minimal Example

```javascript
// my-custom-widget.js - A simple big-number display plugin

(function() {
  // Register the widget type with XACT
  window.XACT.registerWidgetType({
    type: 'my-custom-widget',
    name: 'My Custom Widget',
    icon: '🔧',
    category: 'Custom',
    defaultW: 6,
    defaultH: 4,
    minW: 3,
    minH: 2,
  });

  class MyCustomWidget extends HTMLElement {
    constructor() {
      super();
      this._config = {};
      this._unsubscribe = null;
    }

    // Called by the framework when the widget is placed on a dashboard
    setConfig(config) {
      this._config = config || {};
      this.render();
      this.subscribe();
    }

    render() {
      this.innerHTML = `
        <div style="padding: 16px; text-align: center;">
          <div style="font-size: 2em; font-weight: bold;" id="value">--</div>
          <div style="font-size: 0.8em; opacity: 0.7;">${this._config.label || 'Value'}</div>
        </div>
      `;
    }

    subscribe() {
      if (this._unsubscribe) this._unsubscribe();
      const tagPath = this._config.tagPath;
      if (!tagPath) return;

      // Subscribe to live tag updates via the XACT store
      this._unsubscribe = window.XACT.store.subscribe(tagPath, (value) => {
        const el = this.querySelector('#value');
        if (el) el.textContent = value ?? '--';
      });
    }

    // Called by the framework to get the configuration schema
    getPropertySchema() {
      return [
        { key: 'tagPath', label: 'Tag Path', type: 'tag', required: true },
        { key: 'label', label: 'Label', type: 'text', default: 'Value' },
      ];
    }

    disconnectedCallback() {
      if (this._unsubscribe) {
        this._unsubscribe();
        this._unsubscribe = null;
      }
    }
  }

  customElements.define('my-custom-widget', MyCustomWidget);
})();
```

### Plugin API

Plugin widgets have access to the `window.XACT` bridge object, which provides:

| API | Description |
|-----|-------------|
| `XACT.store.subscribe(path, callback)` | Subscribe to live tag value updates. Returns an unsubscribe function. |
| `XACT.store.getValue(path)` | Get the current cached value of a tag. |
| `XACT.store.listChildrenNames(path)` | List child node names under a path. |
| `XACT.registerWidgetType(metadata)` | Register a new widget type with the toolbar. |
| `XACT.registerMapItemType(name, renderer)` | Register a custom map item type (for map plugins). |
| `XACT.getMapItemType(name)` | Retrieve a registered map item type. |

### Widget Lifecycle

1. **Registration** - `XACT.registerWidgetType()` is called when the script loads, making the widget available in the toolbar.
2. **Instantiation** - when a user drags the widget onto a dashboard, the custom element is created and added to the DOM.
3. **Configuration** - `setConfig(config)` is called with any saved configuration. The widget renders itself and subscribes to data.
4. **Persistence** - when the user saves the dashboard, the widget's configuration is serialized and stored. On next load, `setConfig()` is called with the restored configuration.
5. **Cleanup** - when the widget is removed or the page navigates away, `disconnectedCallback()` is called. Clean up subscriptions and timers here.

### Configuration Schema

If your widget implements `getPropertySchema()`, the framework will generate a configuration dialog automatically. The schema is an array of field definitions:

| Field Property | Description |
|---------------|-------------|
| `key` | Configuration key name |
| `label` | Display label in the config dialog |
| `type` | Input type: `text`, `number`, `tag` (tag tree browser), `select`, `boolean` |
| `required` | Whether the field is required |
| `default` | Default value |
| `options` | For `select` type: array of `{label, value}` objects |

### Permissions

Plugin widgets participate in the standard XACT permissions system. When registering a widget type, you can specify a required permission - users without that permission will not see the widget in the toolbar or on dashboards.

## Authentication Plugins

Authentication plugins provide an alternative login mechanism. If an authentication plugin is present, it replaces the built-in username/password authentication.

Authentication plugins are **Go scripts** executed using the Yaegi interpreter. Place the script in the `plugins/authentication/` directory.

### Example

```go
package plugin

func Authenticate(user, password string) bool {
    // Implement your custom authentication logic here.
    // Return true if the user is authenticated, false otherwise.
    // This could check against LDAP, Active Directory, SSO, etc.
    return true
}
```

The plugin must export an `Authenticate` function that accepts a username and password and returns a boolean.

## Map Item Plugins

> **This feature is planned for a future release.**

Map item plugins will allow custom visualisation elements on the Area Map widget - for example, weather overlays, traffic indicators, or custom device renderers.

## Theme Plugins

Theme plugins allow you to create and distribute custom visual themes. A theme plugin is a JavaScript file placed in the `plugins/themes/` directory.

### How Theme Plugins Work

1. Place a `.js` file in the `plugins/themes/` directory on the server.
2. XACT loads all theme scripts at startup, alongside widget plugins.
3. Each script calls `window.XACT.registerTheme()` to inject its CSS and register the theme.
4. The theme immediately appears in the **Preferences** dialog under Theme Color.
5. If a user had selected the plugin theme in a previous session, it is restored automatically.

### Creating a Theme Plugin

A theme plugin injects a CSS block that defines every XACT CSS variable for a `[data-theme="<id>"]` selector.

```javascript
// my-theme.js
(function () {
  'use strict';

  window.XACT.registerTheme(
    {
      id:      'my-theme',          // unique ID - used as the data-theme attribute value
      name:    'My Theme',          // display name shown in the Preferences dialog
      preview: '#1a1a2e',           // swatch colour shown next to the theme name
    },
    `
    [data-theme="my-theme"] {
      --sidebar-bg:         #1a1a2e;
      --sidebar-text:       #eee;
      --sidebar-hover:      #16213e;
      --sidebar-active:     #0f3460;
      --header-bg:          #16213e;
      --header-text:        #eee;
      --footer-bg:          #16213e;
      --footer-text:        #888;
      --content-bg:         #0f3460;
      --content-text:       #eee;
      --accent-color:       #e94560;
      --accent-hover:       #c73652;
      --accent-text:        #fff;
      --border-color:       #0f3460;
      --modal-bg:           #1a1a2e;
      --modal-text:         #eee;
      --widget-bg:          #16213e;
      --widget-header-bg:   #1a1a2e;
      --widget-border:      #0f3460;
      --widget-shadow:      rgba(0,0,0,0.4);
      --widget-header-text: #eee;
      --widget-icon-hover:  #e94560;
      --danger-color:       #e94560;
      --error-color:        #e94560;
      --error-bg:           rgba(233,69,96,0.1);
      --error-border:       #e94560;
      --status-good-color:  #4ade80;
      --status-good-bg:     rgba(74,222,128,0.1);
      --status-bad-color:   #e94560;
      --status-bad-bg:      rgba(233,69,96,0.1);
      --status-warn-color:  #fb923c;
      --status-warn-bg:     rgba(251,146,60,0.1);
      --status-unknown-color: #334155;
      --dlw-row-hover:      rgba(233,69,96,0.06);
      --dlw-text-dim:       #0f1a2e;
      --dlw-kpi-text:       #556080;
      --input-bg:           rgba(255,255,255,0.04);
      --surface-tint:       rgba(255,255,255,0.02);
      --subtle-divider:     rgba(255,255,255,0.07);
      --code-bg:            rgba(255,255,255,0.08);
      --inactive-dot:       rgba(255,255,255,0.2);
    }
    `
  );
})();
```

### Theme Plugin API

| Property | Description |
|----------|-------------|
| `id` | Unique theme identifier. Used as the `data-theme` attribute value. Must not conflict with built-in theme IDs (`dark-navy`, `green-blue-chill`, `lilac-elegance`, `voltage`, `sandstone`). |
| `name` | Display name shown in the Preferences dialog. |
| `preview` | A CSS colour string used for the swatch circle in the theme list. |
| CSS text | A complete `[data-theme="<id>"] { … }` block defining all required CSS variables. |

### Required CSS Variables

Your theme must define all variables used by the XACT shell and widgets. At minimum: `--sidebar-bg/text/hover/active`, `--header-bg/text`, `--footer-bg/text`, `--content-bg/text`, `--accent-color/hover/text`, `--border-color`, `--modal-bg/text`, `--widget-bg/header-bg/border/shadow/header-text/icon-hover`, `--danger-color`, `--error-color/bg/border`, `--status-good/bad/warn/unknown-*`, `--dlw-*`, `--input-bg`, `--surface-tint`, `--subtle-divider`, `--code-bg`, `--inactive-dot`.
