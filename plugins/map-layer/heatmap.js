/**
 * heatmap - XACT map-layer plugin
 *
 * Aggregates a configured tag from every matched device in an Area Map layer
 * and renders the result into a Leaflet canvas grid covering the map's default
 * bounds. Use with Item Type "plugin" and Plugin Type "heatmap".
 */
(function () {
  'use strict';

  const DEFAULTS = {
    tagPath: 'online',
    gridResolution: 32,
    contributionRadius: 2,
    decayPower: 1.5,
    renderMode: 'circle',
    blobRadius: 1.15,
    edgeSoftness: 0.85,
    scaling: 'linear',
    minValue: 0,
    maxValue: 1,
    lowColor: '#1d4ed8',
    midColor: '#facc15',
    highColor: '#dc2626',
    opacity: 0.55,
  };

  function getPropertySchema() {
    return [
      {
        name: 'tagPath',
        type: 'path',
        label: 'Device Tag',
        description: 'Relative tag path on each matched device, e.g. online or kpi.temperature. An absolute org path is also accepted.',
        default: DEFAULTS.tagPath,
        context: { includeLeaves: true },
      },
      {
        name: 'gridResolution',
        type: 'number',
        label: 'Grid Resolution',
        description: 'Number of grid cells along the larger bounds axis.',
        default: DEFAULTS.gridResolution,
        context: { min: 4, max: 256, step: 1 },
      },
      {
        name: 'contributionRadius',
        type: 'number',
        label: 'Contribution Radius',
        description: 'How many neighboring grid cells a true value influences.',
        default: DEFAULTS.contributionRadius,
        context: { min: 0, max: 32, step: 0.25 },
      },
      {
        name: 'decayPower',
        type: 'number',
        label: 'Decay Power',
        description: 'Higher values make contribution fade faster with distance.',
        default: DEFAULTS.decayPower,
        context: { min: 0.1, max: 8, step: 0.1 },
      },
      {
        name: 'renderMode',
        type: 'select',
        label: 'Render Mode',
        description: 'Circle uses soft circular blobs; square keeps the older grid-cell rendering.',
        default: DEFAULTS.renderMode,
        context: {
          options: [
            { value: 'circle', label: 'Soft circles' },
            { value: 'square', label: 'Square cells' },
          ],
        },
      },
      {
        name: 'blobRadius',
        type: 'number',
        label: 'Blob Radius',
        description: 'Circle radius as a multiplier of the average grid-cell size.',
        default: DEFAULTS.blobRadius,
        context: { min: 0.05, max: 12, step: 0.05 },
      },
      {
        name: 'edgeSoftness',
        type: 'number',
        label: 'Edge Softness',
        description: '0 gives a hard circle edge; 1 fades smoothly to transparent.',
        default: DEFAULTS.edgeSoftness,
        context: { min: 0, max: 1, step: 0.05 },
      },
      {
        name: 'scaling',
        type: 'select',
        label: 'Scaling',
        description: 'How accumulated cell values are scaled before color mapping.',
        default: DEFAULTS.scaling,
        context: {
          options: [
            { value: 'linear', label: 'Linear' },
            { value: 'sqrt', label: 'Square root' },
            { value: 'log', label: 'Logarithmic' },
          ],
        },
      },
      { name: 'minValue', type: 'number', label: 'Minimum Value', default: DEFAULTS.minValue, context: { step: 0.1 } },
      { name: 'maxValue', type: 'number', label: 'Maximum Value', default: DEFAULTS.maxValue, context: { step: 0.1 } },
      { name: 'lowColor', type: 'color', label: 'Low Color', default: DEFAULTS.lowColor },
      { name: 'midColor', type: 'color', label: 'Mid Color', default: DEFAULTS.midColor },
      { name: 'highColor', type: 'color', label: 'High Color', default: DEFAULTS.highColor },
      {
        name: 'opacity',
        type: 'number',
        label: 'Opacity',
        description: '0 is transparent, 1 is fully opaque.',
        default: DEFAULTS.opacity,
        context: { min: 0, max: 1, step: 0.05 },
      },
    ];
  }

  function create(context) {
    return new HeatmapLayer(context);
  }

  class HeatmapLayer {
    constructor(context) {
      this.context = context;
      this.map = context.map;
      this.L = context.L;
      this.store = context.store;
      this.config = normalizeConfig(context.config);
      this.devices = [];
      this.active = true;
      this.generation = 0;
      this.canvasLayer = this.createCanvasLayer();
      this.canvasLayer.addTo(this.map);
      this.boundRedraw = () => this.redraw();
      this.map.on('moveend zoomend resize', this.boundRedraw);
    }

    setConfig(config) {
      this.config = normalizeConfig(config);
      this.redraw();
    }

    updateDevices(devicePaths) {
      this.devices = Array.isArray(devicePaths) ? devicePaths.slice() : [];
      const gen = ++this.generation;
      for (const devicePath of this.devices) {
        this.subscribeDevice(devicePath, gen);
      }
      this.redraw();
    }

    remove() {
      this.active = false;
      this.generation++;
      this.map.off('moveend zoomend resize', this.boundRedraw);
      this.canvasLayer.remove();
    }

    subscribeDevice(devicePath, gen) {
      const redraw = () => {
        if (!this.active || gen !== this.generation) return;
        this.redraw();
      };
      this.store.subscribe(devicePath + '.meta.lat', redraw);
      this.store.subscribe(devicePath + '.meta.lon', redraw);
      this.store.subscribe(this.context.resolveDeviceTag(devicePath, this.config.tagPath), redraw);
    }

    createCanvasLayer() {
      const self = this;
      const CanvasHeatmap = this.L.Layer.extend({
        onAdd(map) {
          this._map = map;
          this._canvas = self.L.DomUtil.create('canvas', 'xact-heatmap-layer');
          this._canvas.style.position = 'absolute';
          this._canvas.style.pointerEvents = 'none';
          this._canvas.style.mixBlendMode = 'multiply';
          const paneName = self.context.paneName || self.context.pane;
          const pane = paneName && map.getPane(paneName) ? map.getPane(paneName) : map.getPanes().overlayPane;
          pane.appendChild(this._canvas);
          this._reset = () => self.redraw();
          map.on('move zoom resize', this._reset);
          self.redraw();
        },
        onRemove(map) {
          map.off('move zoom resize', this._reset);
          this._canvas?.remove();
        },
      });
      return new CanvasHeatmap();
    }

    redraw() {
      if (!this.active || !this.canvasLayer?._canvas || !this.map) return;

      const canvas = this.canvasLayer._canvas;
      const size = this.map.getSize();
      const topLeft = this.map.containerPointToLayerPoint([0, 0]);
      this.L.DomUtil.setPosition(canvas, topLeft);
      canvas.width = Math.max(1, Math.floor(size.x));
      canvas.height = Math.max(1, Math.floor(size.y));

      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.clearRect(0, 0, canvas.width, canvas.height);

      const bounds = this.context.getBounds();
      const north = Number(bounds.north);
      const south = Number(bounds.south);
      const east = Number(bounds.east);
      const west = Number(bounds.west);
      if (![north, south, east, west].every(Number.isFinite) || north <= south || east <= west) return;

      const lonSpan = east - west;
      const latSpan = north - south;
      const resolution = clampInt(this.config.gridResolution, 4, 256);
      const cols = lonSpan >= latSpan ? resolution : Math.max(4, Math.round(resolution * lonSpan / latSpan));
      const rows = latSpan > lonSpan ? resolution : Math.max(4, Math.round(resolution * latSpan / lonSpan));
      const grid = Array.from({ length: rows }, () => Array(cols).fill(0));
      const contributionRadius = clamp(Number(this.config.contributionRadius), 0, 32);
      const decayPower = clamp(Number(this.config.decayPower), 0.1, 8);

      for (const devicePath of this.devices) {
        const lat = Number(this.store.getNodeValue(devicePath + '.meta.lat'));
        const lon = Number(this.store.getNodeValue(devicePath + '.meta.lon'));
        if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
        if (lat < south || lat > north || lon < west || lon > east) continue;

        const raw = this.store.getNodeValue(this.context.resolveDeviceTag(devicePath, this.config.tagPath));
        const value = toNumber(raw);
        if (!Number.isFinite(value)) continue;

        const col = Math.min(cols - 1, Math.max(0, Math.floor((lon - west) / lonSpan * cols)));
        const row = Math.min(rows - 1, Math.max(0, Math.floor((north - lat) / latSpan * rows)));
        addContribution(grid, row, col, value, contributionRadius, decayPower);
      }

      const minValue = Number(this.config.minValue);
      const maxValue = Number(this.config.maxValue);
      const min = Number.isFinite(minValue) ? minValue : 0;
      const max = Number.isFinite(maxValue) && maxValue > min ? maxValue : min + 1;
      const opacity = clamp(Number(this.config.opacity), 0, 1);
      const renderMode = this.config.renderMode;
      const blobRadius = clamp(Number(this.config.blobRadius), 0.05, 12);
      const edgeSoftness = clamp(Number(this.config.edgeSoftness), 0, 1);

      for (let row = 0; row < rows; row++) {
        for (let col = 0; col < cols; col++) {
          const value = grid[row][col];
          if (value <= min) continue;
          const t = scaleValue(value, min, max, this.config.scaling);
          const color = colorRamp(t, this.config.lowColor, this.config.midColor, this.config.highColor);

          const cellWest = west + (col / cols) * lonSpan;
          const cellEast = west + ((col + 1) / cols) * lonSpan;
          const cellNorth = north - (row / rows) * latSpan;
          const cellSouth = north - ((row + 1) / rows) * latSpan;
          const p1 = this.map.latLngToContainerPoint([cellNorth, cellWest]);
          const p2 = this.map.latLngToContainerPoint([cellSouth, cellEast]);

          if (renderMode === 'square') {
            ctx.fillStyle = `rgba(${color.r},${color.g},${color.b},${opacity * opacityForValue(t)})`;
            ctx.fillRect(
              Math.floor(p1.x),
              Math.floor(p1.y),
              Math.ceil(p2.x - p1.x) + 1,
              Math.ceil(p2.y - p1.y) + 1
            );
          } else {
            const width = Math.abs(p2.x - p1.x);
            const height = Math.abs(p2.y - p1.y);
            const cx = (p1.x + p2.x) / 2;
            const cy = (p1.y + p2.y) / 2;
            const radius = Math.max(1, (width + height) * 0.25 * blobRadius);
            drawBlob(ctx, cx, cy, radius, color, opacity * opacityForValue(t), edgeSoftness);
          }
        }
      }
    }
  }

  function normalizeConfig(config) {
    const cfg = Object.assign({}, DEFAULTS, config || {});
    cfg.gridResolution = clampInt(cfg.gridResolution, 4, 256);
    cfg.contributionRadius = clamp(Number(cfg.contributionRadius), 0, 32);
    cfg.decayPower = clamp(Number(cfg.decayPower), 0.1, 8);
    cfg.renderMode = ['circle', 'square'].includes(cfg.renderMode) ? cfg.renderMode : 'circle';
    cfg.blobRadius = clamp(Number(cfg.blobRadius), 0.05, 12);
    cfg.edgeSoftness = clamp(Number(cfg.edgeSoftness), 0, 1);
    cfg.opacity = clamp(Number(cfg.opacity), 0, 1);
    cfg.scaling = ['linear', 'sqrt', 'log'].includes(cfg.scaling) ? cfg.scaling : 'linear';
    return cfg;
  }

  function drawBlob(ctx, cx, cy, radius, color, alpha, edgeSoftness) {
    if (edgeSoftness <= 0) {
      ctx.fillStyle = `rgba(${color.r},${color.g},${color.b},${alpha})`;
      ctx.beginPath();
      ctx.arc(cx, cy, radius, 0, Math.PI * 2);
      ctx.fill();
      return;
    }

    const innerRadius = radius * (1 - edgeSoftness);
    const gradient = ctx.createRadialGradient(cx, cy, innerRadius, cx, cy, radius);
    gradient.addColorStop(0, `rgba(${color.r},${color.g},${color.b},${alpha})`);
    gradient.addColorStop(1, `rgba(${color.r},${color.g},${color.b},0)`);
    ctx.fillStyle = gradient;
    ctx.beginPath();
    ctx.arc(cx, cy, radius, 0, Math.PI * 2);
    ctx.fill();
  }

  function opacityForValue(t) {
    return 0.25 + 0.75 * clamp(t, 0, 1);
  }

  function addContribution(grid, row, col, value, radius, decayPower) {
    if (value <= 0) return;
    if (radius <= 0) {
      grid[row][col] += value;
      return;
    }

    const rows = grid.length;
    const cols = grid[0]?.length ?? 0;
    const maxOffset = Math.ceil(radius);
    for (let dr = -maxOffset; dr <= maxOffset; dr++) {
      const r = row + dr;
      if (r < 0 || r >= rows) continue;

      for (let dc = -maxOffset; dc <= maxOffset; dc++) {
        const c = col + dc;
        if (c < 0 || c >= cols) continue;

        const distance = Math.sqrt(dr * dr + dc * dc);
        if (distance > radius) continue;

        const weight = Math.pow(1 - distance / (radius + 1), decayPower);
        grid[r][c] += value * weight;
      }
    }
  }

  function toNumber(value) {
    if (value === true) return 1;
    if (value === false || value === null || value === undefined) return 0;
    const n = Number(value);
    return Number.isFinite(n) ? n : 0;
  }

  function scaleValue(value, min, max, scaling) {
    const clamped = clamp((value - min) / (max - min), 0, 1);
    if (scaling === 'sqrt') return Math.sqrt(clamped);
    if (scaling === 'log') return Math.log1p(clamped * 9) / Math.log(10);
    return clamped;
  }

  function colorRamp(t, low, mid, high) {
    const a = t < 0.5 ? hexToRgb(low) : hexToRgb(mid);
    const b = t < 0.5 ? hexToRgb(mid) : hexToRgb(high);
    const localT = t < 0.5 ? t * 2 : (t - 0.5) * 2;
    return {
      r: Math.round(a.r + (b.r - a.r) * localT),
      g: Math.round(a.g + (b.g - a.g) * localT),
      b: Math.round(a.b + (b.b - a.b) * localT),
    };
  }

  function hexToRgb(hex) {
    const fallback = { r: 245, g: 158, b: 11 };
    const s = String(hex || '').trim();
    const m = /^#?([0-9a-f]{6})$/i.exec(s);
    if (!m) return fallback;
    const n = parseInt(m[1], 16);
    return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
  }

  function clamp(value, min, max) {
    if (!Number.isFinite(value)) return min;
    return Math.min(max, Math.max(min, value));
  }

  function clampInt(value, min, max) {
    return Math.round(clamp(Number(value), min, max));
  }

  if (!window.XACT) {
    console.error('[heatmap] window.XACT bridge not found');
    return;
  }

  window.XACT.registerMapLayerType('heatmap', {
    name: 'Heatmap',
    defaultConfig: DEFAULTS,
    getPropertySchema,
    create,
  });
})();
