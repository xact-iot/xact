// Run before the application to avoid a flash of the wrong theme.
try {
  document.documentElement.setAttribute('data-theme', localStorage.getItem('xact-theme') || 'dark-navy');
  document.documentElement.setAttribute('data-widget-decoration', localStorage.getItem('xact-widget-decoration') || 'shadowed');
} catch { /* Storage may be disabled by browser policy. */ }
if (new URLSearchParams(window.location.search).get('embedded') === 'dashboard') {
  document.documentElement.classList.add('xact-embedded-dashboard');
}
