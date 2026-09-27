if ((window.location.pathname === '/xact/public' || window.location.pathname.startsWith('/xact/public/'))) {
  void import('./public-page');
} else {
  void import('./main');
}
