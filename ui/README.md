# XACT UI

Web interface for the XACT traffic control system.

## Tech Stack

- **Web Components** - Custom elements without Shadow DOM
- **TypeScript** - Type-safe JavaScript
- **Tailwind CSS** - Utility-first CSS framework
- **Vite** - Fast development server and bundler

## Project Structure

```
ui/
├── src/
│   ├── components/      # Web components
│   │   ├── base-component.ts    # Base class for all components
│   │   ├── app-sidebar.ts       # Sidebar navigation
│   │   ├── app-header.ts        # Header bar
│   │   ├── app-footer.ts        # Footer bar
│   │   └── app-content.ts       # Main content area
│   ├── themes/
│   │   └── theme-manager.ts     # Theme switching system
│   ├── styles.css       # Global styles and CSS variables
│   └── main.ts          # Application entry point
├── index.html           # Main HTML file
├── package.json         # Dependencies and scripts
├── tsconfig.json        # TypeScript configuration
├── vite.config.ts       # Vite configuration
├── tailwind.config.js   # Tailwind CSS configuration
└── postcss.config.js    # PostCSS configuration
```

## Development

Install dependencies:

```bash
npm install
```

Start the development server:

```bash
npm run dev
```

The dev server will start on http://localhost:3000

## Building for Production

```bash
npm run build
```

The built files will be in the `dist/` directory.

## Features

### Layout
- **Sidebar**: Left-side navigation menu with collapsible support on mobile
- **Header**: Top bar with page title, theme toggle, and user info
- **Content**: Main content area with panel switching
- **Footer**: Bottom bar with connection status and system info

### Theming
- Light and dark themes available
- Theme switching via header button or settings panel
- Theme preference saved to localStorage
- CSS custom properties for easy theming

### Panels
- Dashboard - System overview with stats and recent activity
- Devices - Device management (placeholder)
- Tags - Tag browser (placeholder)
- Templates - Template management (placeholder)
- Settings - System settings including theme selection

### Responsive Design
- Sidebar collapses on mobile devices (< 768px)
- Grid layout adapts to different screen sizes
- Touch-friendly interface elements

## Web Components

All UI areas are implemented as web components without Shadow DOM:

- `<app-sidebar>` - Navigation sidebar
- `<app-header>` - Top header bar
- `<app-content>` - Main content area with panel switching
- `<app-footer>` - Bottom footer bar

### Creating New Panels

To add a new panel to the content area:

```typescript
// In app-content.ts
this.registerPanel({
  id: 'my-panel',
  title: 'My Panel',
  render: () => `
    <div>My panel content</div>
  `
});
```

Then add a menu item to the sidebar:

```typescript
// In app-sidebar.ts
{ id: 'my-panel', label: 'My Panel', icon: '📄', panel: 'my-panel' }
```

## Architecture

- Components extend `BaseComponent` for consistent lifecycle management
- Theme system uses CSS custom properties for dynamic theming
- Panel system allows easy extension with new views
- Event-based communication between components
- No Shadow DOM to allow Tailwind CSS styling

## Browser Support

Modern browsers supporting:
- Custom Elements v1
- ES2020
- CSS Grid
- CSS Custom Properties
