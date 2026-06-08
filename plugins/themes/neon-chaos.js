/**
 * neon-chaos - XACT Theme Plugin
 *
 * A full-spectrum neon assault on every surface.  Each zone of the UI gets its
 * own distinct neon hue, ensuring maximum chromatic incoherence at all times.
 *
 *   Sidebar ......... Matrix green  (#00ff41)
 *   Header .......... Neon cyan     (#00ffff)
 *   Content text .... Neon cyan     (#00ffff)
 *   Accent / focus .. Neon magenta  (#ff00ff)
 *   Widget borders .. Neon green    (#00ff41)
 *   Widget headers .. Hot magenta   (#ff00ff)
 *   Hover icons ..... Neon yellow   (#ffff00)
 *   Status good ..... Green         (#00ff41)
 *   Status bad ...... Neon red-pink (#ff0066)
 *   Status warn ..... Neon yellow   (#ffff00)
 *
 * All backgrounds are near-black so the neon colours blaze like signs in the rain.
 */
(function () {
  'use strict';

  const THEME_ID = 'neon-chaos';

  const CSS = `
[data-theme="${THEME_ID}"] {
  /* ── Backgrounds: pure near-black throughout ── */
  --sidebar-bg:         #050905;
  --header-bg:          #040408;
  --footer-bg:          #040408;
  --content-bg:         #060606;
  --modal-bg:           #080808;
  --widget-bg:          #080008;
  --widget-header-bg:   #050005;

  /* ── Text: each zone bleeds a different neon ── */
  --sidebar-text:       #00ff41;
  --header-text:        #00ffff;
  --footer-text:        #00cc33;
  --content-text:       #00ffff;
  --modal-text:         #ffff00;
  --widget-header-text: #ff00ff;

  /* ── Sidebar interaction ── */
  --sidebar-hover:      #001a08;
  --sidebar-active:     #00ff41;

  /* ── Accent: neon magenta ── */
  --accent-color:       #ff00ff;
  --accent-hover:       #ff66ff;
  --accent-text:        #000000;

  /* ── Borders ── */
  --border-color:       #ff00ff;
  --widget-border:      #00ff41;

  /* ── Widget chrome ── */
  --widget-shadow:      0 0 12px rgba(0, 255, 65, 0.35), 0 0 24px rgba(255, 0, 255, 0.15);
  --widget-icon-hover:  #ffff00;

  /* ── Danger / error ── */
  --danger-color:       #ff0066;
  --error-color:        #ff0066;
  --error-bg:           rgba(255, 0, 102, 0.15);
  --error-border:       #ff0066;

  /* ── Semantic status ── */
  --status-good-color:    #00ff41;
  --status-good-bg:       rgba(0, 255, 65, 0.12);
  --status-bad-color:     #ff0066;
  --status-bad-bg:        rgba(255, 0, 102, 0.12);
  --status-warn-color:    #ffff00;
  --status-warn-bg:       rgba(255, 255, 0, 0.10);
  --status-unknown-color: #1a1a1a;

  /* ── Device list widget ── */
  --dlw-row-hover: rgba(255, 0, 255, 0.08);
  --dlw-text-dim:  #0d000d;
  --dlw-kpi-text:  #cc00cc;

  /* ── Adaptive surfaces (dark-on-dark) ── */
  --input-bg:       rgba(255, 0, 255, 0.06);
  --surface-tint:   rgba(0, 255, 65, 0.03);
  --subtle-divider: rgba(255, 0, 255, 0.18);
  --code-bg:        rgba(0, 255, 65, 0.08);
  --inactive-dot:   rgba(255, 0, 255, 0.25);
}

/* ── Neon glow keyframes for widget borders ── */
@keyframes neon-chaos-pulse {
  0%   { box-shadow: 0 0 4px #00ff41,  0 0 8px  #00ff41; }
  33%  { box-shadow: 0 0 4px #ff00ff,  0 0 8px  #ff00ff; }
  66%  { box-shadow: 0 0 4px #00ffff,  0 0 8px  #00ffff; }
  100% { box-shadow: 0 0 4px #00ff41,  0 0 8px  #00ff41; }
}

[data-theme="${THEME_ID}"] .widget-card {
  animation: neon-chaos-pulse 4s ease-in-out infinite;
}

/* ── Glowing active sidebar item ── */
[data-theme="${THEME_ID}"] .sidebar-active,
[data-theme="${THEME_ID}"] [aria-current="page"] {
  text-shadow: 0 0 6px #00ff41, 0 0 12px #00ff41;
}

/* ── Scrollbar: neon green on black ── */
[data-theme="${THEME_ID}"] ::-webkit-scrollbar-thumb {
  background: #00ff41;
  box-shadow: 0 0 4px #00ff41;
}

[data-theme="${THEME_ID}"] ::-webkit-scrollbar-thumb:hover {
  background: #ff00ff;
  box-shadow: 0 0 6px #ff00ff;
}

/* ── Input focus rings ── */
[data-theme="${THEME_ID}"] input:focus,
[data-theme="${THEME_ID}"] select:focus,
[data-theme="${THEME_ID}"] textarea:focus {
  outline: 1px solid #ff00ff;
  box-shadow: 0 0 0 2px rgba(255, 0, 255, 0.3), 0 0 8px #ff00ff;
}
`;

  if (!window.XACT) {
    console.error('[neon-chaos] window.XACT bridge not found - ensure loader.ts ran first');
    return;
  }

  window.XACT.registerTheme(
    {
      id:      THEME_ID,
      name:    'Neon Chaos',
      preview: '#050905',
    },
    CSS
  );

})();
