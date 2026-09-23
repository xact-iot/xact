import { afterEach, describe, expect, it, vi } from 'vitest';
import { escapeSelector, sanitizeHtml } from '../src/utils/html-sanitize';
import { csvCell } from '../src/utils/csv';
import { renderMapTemplate } from '../src/utils/map-template';
import { normalizeSvgDiagramConfig, parseSvgTemplate } from '../src/dashboards/widgets/svg-diagram-widget';
import { getMirrorStore } from '../src/store/store';
import '../src/dashboards/widgets/text-widget';
import '../src/dashboards/widgets/html-widget';
import '../src/dashboards/widgets/tags-manager-widget';
import '../src/dashboards/widgets/timeseries-chart-widget';
import '../src/dashboards/widgets/pdf-template-widget';
import '../src/components/app-sidebar';

const payload = '<img src="/missing" onerror="window.securityProbe = true">';
const context = { deviceName: 'Pump 1', deviceDescription: 'Feed', tag: (path: string) => path === 'temperature' ? 120 : payload };

afterEach(() => { document.body.replaceChildren(); vi.restoreAllMocks(); });

describe('untrusted map templates', () => {
  it('preserves variables, tag references, comparisons, arithmetic and conditionals', () => {
    expect(renderMapTemplate('${deviceName} ${deviceDescription}: ${tag("temperature") + 2}', context)).toBe('Pump 1 Feed: 122');
    expect(renderMapTemplate('<b style="color:${tag("temperature") > 100 ? "red" : "green"}">${tag("temperature")}</b>', context)).toContain('color:red');
    const div = document.createElement('div'); div.innerHTML = renderMapTemplate('${tag(sign.message)}', context);
    expect(div.textContent).toBe(payload);
    expect(div.querySelector('img')).toBeNull();
  });

  it.each([
    '${localStorage.getItem("xact_auth_token")}',
    '${window.location}',
    '${tag.constructor("return window")()}',
    '${tag("temperature").constructor}',
    '${(() => { window.securityProbe = true })()}',
    '${deviceName = "owned"}',
    '${[deviceName]}',
    '${this}',
    '${tag(deviceName)}',
  ])('rejects executable or unsupported expression %s', template => {
    expect(() => renderMapTemplate(template, context)).toThrow();
  });

  it('escapes substitution results and sanitizes static template markup', () => {
    const rendered = renderMapTemplate(payload + '${tag("message")}', context);
    const div = document.createElement('div'); div.innerHTML = rendered;
    expect(div.querySelector('[onerror]')).toBeNull();
    expect(div.querySelectorAll('img')).toHaveLength(1); // static safe image, never the tag value
    expect(div.textContent).toContain(payload);
  });
});

describe('rich content boundaries', () => {
  it('removes active custom elements and configuration attributes', () => {
    const child = document.createElement('text-widget');
    child.setAttribute('config', JSON.stringify({ color: '">' + payload }));
    const div = document.createElement('div'); div.innerHTML = sanitizeHtml(child.outerHTML);
    expect(div.querySelector('text-widget')).toBeNull();
    expect(div.querySelector('[config], [onerror]')).toBeNull();
  });

  it('blocks scripts, frames, stylesheets and dangerous links without losing formatting', () => {
    const html = sanitizeHtml('<b>ok</b><script>alert(1)</script><iframe src="https://example.com"></iframe><style>body{display:none}</style><a href="javascript:alert(1)">link</a>');
    const div = document.createElement('div'); div.innerHTML = html;
    expect(div.querySelector('b')?.textContent).toBe('ok');
    expect(div.querySelector('script,iframe,style,[href]')).toBeNull();
  });

  it('sanitizes SVG from saved JSON as well as files', () => {
    const source = '<rect width="10" height="10" onload="bad()"/><script>bad()</script><set attributeName="href" to="javascript:bad()"/>';
    const cfg = normalizeSvgDiagramConfig({ templateSvg: { viewBox: '0 0 20 20', content: source } });
    expect(cfg.templateSvg?.content).toContain('<rect');
    expect(cfg.templateSvg?.content).not.toMatch(/<script|<set|onload|javascript:/);
    expect(normalizeSvgDiagramConfig({ templateSvg: { viewBox: '0 0 20 20', content: '</svg>' + payload } }).templateSvg).toBeUndefined();
  });

  it('removes SVG URL mutation and external resources while retaining drawing geometry', () => {
    const imported = parseSvgTemplate('<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/><image href="https://example.com/image.png"/><animate attributeName="href" values="#safe;javascript:bad()"/></svg>');
    expect(imported.template.content).toContain('<rect');
    expect(imported.template.content).not.toMatch(/<animate|javascript:|https:\/\/example/);
  });
});

describe('plain data rendering', () => {
  it('escapes stored tag data in the value editor and pipeline debugger', () => {
    const widget = document.createElement('tags-manager-widget') as any;
    widget.isValueEditOpen = true;
    widget.valueEditPath = 'audit.device.message';
    widget.valueEditCurrent = '">' + payload;
    widget.isDebuggerOpen = true;
    widget.debugResults = [{ type: 'publish', input: payload, output: payload, error: payload }];
    widget.debugInput = '">' + payload;
    widget.debugFinalOutput = payload;
    const div = document.createElement('div');
    div.innerHTML = widget.renderValueEditModal() + widget.renderDebugger();
    expect(div.querySelector('img,[onerror]')).toBeNull();
    expect(div.textContent).toContain(payload);
    expect(div.querySelector<HTMLInputElement>('#value-edit-input')?.value).toBe('\">' + payload);
  });

  it('escapes image URLs and styles from saved report templates', () => {
    const widget = document.createElement('pdf-template-widget') as any;
    const div = document.createElement('div');
    div.innerHTML = widget.renderCanvasImage({ imageData: '/missing\" onerror=\"bad()' }, 100, 1)
      + widget.renderCanvasGrid({ type: 'table', rows: [[{ text: 'safe', bgColor: '\">' + payload, font: '\">' + payload }]] }, 100, 1, 0);
    expect(div.querySelector('[onerror]')).toBeNull();
    expect(div.querySelectorAll('img')).toHaveLength(1);
  });

  it.each(['=1+1', '+1+1', '-1+1', '@SUM(1)', '\t=1+1', '  =1+1', '＝1+1'])('neutralizes spreadsheet formula %j', value => {
    expect(csvCell(value)).toBe('"\'' + value + '"');
  });

  it('retains CSV quoting for ordinary text and multiline values', () => {
    expect(csvCell('a,"b"\nc')).toBe('"a,""b""\nc"');
    expect(csvCell(42)).toBe('"42"');
  });
  it('renders tag values and units as text on first render and live updates', () => {
    const store = getMirrorStore() as any;
    const path = 'audit.device.security_tag';
    store.applyTagMetadataToNode(path, { value: payload, shared: { units: payload } });
    const widget = document.createElement('tags-manager-widget') as any;
    vi.spyOn(widget, 'connectedCallback').mockImplementation(() => {});
    document.body.append(widget);
    widget.innerHTML = widget.renderLeafTable([path], 0);
    expect(widget.querySelector('img')).toBeNull();
    expect(widget.textContent).toContain(payload);
    store.applyTagMetadataToNode(path, { value: payload + ' updated', shared: { units: payload } });
    expect(widget.querySelector('img')).toBeNull();
    expect(widget.textContent).toContain('updated');
  });

  it('does not interpret style configuration as HTML', () => {
    const widget = document.createElement('text-widget') as any;
    widget.setConfig({ color: 'red;">' + payload, fontSize: '">' + payload, textAlign: '">' + payload });
    expect(widget.querySelector('img,[onerror]')).toBeNull();
    expect(widget.querySelector('.tw-text-content')).not.toBeNull();
  });

  it('escapes organisation metadata in image attributes', () => {
    const sidebar = document.createElement('app-sidebar') as any;
    sidebar.currentOrg = 'audit';
    sidebar.orgDetails.set('audit', { name: 'audit', logo: '/missing" onerror="bad()', displayName: '" onload="bad()' });
    sidebar.render();
    expect(sidebar.querySelector('[onerror],[onload]')).toBeNull();
    expect(sidebar.querySelector('img')?.getAttribute('src')).toBe('/missing" onerror="bad()');
  });

  it('escapes chart series names and colors in HTML tooltips', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    const tooltip = widget.buildChartOption().tooltip.formatter([{ value: [0, 42], seriesName: payload, color: '">' + payload }]);
    const div = document.createElement('div'); div.innerHTML = tooltip;
    expect(div.querySelector('img,[onerror]')).toBeNull();
    expect(div.textContent).toContain(payload);
  });

  it('handles quotes in tag selectors without selecting other elements', () => {
    const div = document.createElement('div'); const child = document.createElement('span');
    const path = 'audit.device.a"], [data-privileged="yes';
    child.setAttribute('data-path', path); div.append(child);
    expect(div.querySelector(`[data-path="${escapeSelector(path)}"]`)).toBe(child);
  });
});
