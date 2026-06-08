/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{ts,js}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Custom theme colors that will be overridden by CSS variables
        'sidebar-bg': 'var(--sidebar-bg)',
        'sidebar-text': 'var(--sidebar-text)',
        'sidebar-hover': 'var(--sidebar-hover)',
        'sidebar-active': 'var(--sidebar-active)',
        'header-bg': 'var(--header-bg)',
        'header-text': 'var(--header-text)',
        'footer-bg': 'var(--footer-bg)',
        'footer-text': 'var(--footer-text)',
        'content-bg': 'var(--content-bg)',
        'content-text': 'var(--content-text)',
        'accent': 'var(--accent-color)',
        'accent-hover': 'var(--accent-hover)',
      },
    },
  },
  plugins: [],
}
