# UI security audit and remediation

Audited 2026-09-23, starting from `9089310` (server security updates). The user subsequently authorized fixes. Some fixes were committed as `faba05c` during the work; the remaining changes and this report complete the same audit.

The primary risk was stored cross-site scripting: lower-trust device data and saved dashboard configuration could execute code in a viewer's browser. The application stores its bearer token in `localStorage`, so successful script injection could read the viewer's token and make requests with their permissions. A privileged viewer opening affected content supplied the escalation opportunity; simply changing client-side role flags did not bypass server authorization.

Nine isolated Chromium proofs read a **synthetic** session marker before remediation. All nine are blocked after remediation, including when tested under the old, permissive script policy. These are nine execution paths, grouped into findings below, rather than nine independent root causes. No production accounts, real tokens, live device commands, or external exfiltration endpoints were used.

## Findings and fixes

| ID | Severity / condition | Finding | Resolution |
| --- | --- | --- | --- |
| UI-01 | High | Tag values, units, node descriptions, and related dialogs were interpolated into HTML. | Escaped plain data in initial renders, live updates, node/tag forms, confirmations, and pipeline debug output; escaped tag selectors. |
| UI-02 | High | Map marker templates executed arbitrary JavaScript and inserted tag results as HTML. | Replaced `new Function` with a restricted expression parser and interpreter; escaped substitutions and sanitized final markup. |
| UI-03 | High | Saved dashboard/report configuration could escape HTML attributes or insert executable SVG/tooltip content. | Escaped configuration at rendering boundaries; normalized text size/alignment; sanitized saved SVG as well as imported SVG. |
| UI-04 | High, when another affected widget was registered | The HTML sanitizer allowed custom elements with `config` attributes, allowing nested widgets to recreate unsafe HTML after sanitization. | Replaced the custom sanitizer with DOMPurify restricted to presentation HTML; excluded custom elements, configuration attributes, scripts, frames, forms, and stylesheets. |
| UI-05 | Medium under default roles; high with delegated branding access | Organisation logo/label attributes used a text-only escaper that preserved quotes. | Applied quote-aware attribute escaping to sidebar metadata and header text/attributes. |
| UI-06 | Medium, requires opening exported CSV | Device values and column headings could become spreadsheet formulas; CSV quoting did not neutralize formula prefixes. | Added formula neutralization before CSV quoting. XLSX event exports retain explicit string cell types. |
| UI-07 | Conditional dependency risks | The original lockfile had 17 affected package entries, including critical development-tool advisories. | Updated vulnerable dependencies; final npm lockfile audit reports zero known vulnerabilities. |
| UI-08 | Defense in depth / supply-chain exposure | Runtime CDN scripts executed with session access; CSP did not restrict scripts; the production build included a browser test runner. | Bundled executable vendors locally, restricted scripts to the application origin, removed inline handlers and the inline startup script, and removed production test-runner build/serving routes. |

### UI-01: Device data interpreted as HTML

Affected paths include `ui/src/dashboards/widgets/tags-manager-widget.ts` (`formatValue`, `ensureSubscribed`, `renderLeafTable`, node/value editors, and pipeline debugger), and metadata in `tree-browser-dialog.ts`.

An attacker able to supply a string tag value could use markup such as `<img src="/missing" onerror="window.auditProbe=true">`. Previously, both the cached table renderer and subscription callback assigned the value to `innerHTML`. Units had the same problem. The browser proofs confirmed execution from value and unit fields; regression tests now cover both initial display and live updates, plus the value editor/debugger.

Exposure requires the victim to view the affected data. A tag writer, such as the default Technician role, does not need dashboard editing permission for the tag-value path. A publisher with valid ingestion access can also influence displayed values. Metadata changes require the corresponding write permission. Browser tests injected synthetic data into the actual mirror-store/component methods; authorization and ingestion reachability were traced in source, not exercised against a deployed server.

### UI-02: Map templates acted as programs

`map-widget.ts::renderTemplateContent` previously constructed a JavaScript template literal and passed it to `new Function`. A saved expression such as `${localStorage.getItem('xact_auth_token')}` therefore executed with the viewer's session access. Independently, a normal `${tag('message')}` expression returned unescaped device content to Leaflet's HTML marker/tooltip rendering.

A dashboard editor (the default Manager role has `dashboards-setup.edit`) could persist an executable template for another same-organisation viewer. The tag-value path needed only control of a referenced value in an existing template. Both paths executed in the isolated browser proofs.

The replacement in `ui/src/utils/map-template.ts` parses expressions into syntax trees with JSEP and evaluates only a small permitted set. It has no global lookup, property access, assignment, object construction, arbitrary function calls, or JavaScript code generation. It also bounds template/expression size and interpreter depth. Rejected templates display the existing escaped device-name fallback.

### UI-03: Stored configuration bypassed rendering assumptions

HTML inputs and TypeScript types do not validate stored JSON. Confirmed browser cases included:

- A `text-widget` color containing a closing quote and an image handler escaped its inline style attribute.
- `svg-diagram-widget` sanitized uploaded files but accepted `templateSvg.content` unchanged from saved JSON.
- The application's custom chart tooltip inserted `seriesName` as HTML. This path required displaying a tooltip.

The same escaping pattern was corrected in dashboard names/descriptions/forms, widget colors and identifiers, status/metric/sparkline rendering, map settings, tab and array configuration, visual graph coordinates, report image/style previews, API error displays, and tag calculation results. These related cases were identified through source review; they are not all claimed as separate browser-reproduced exploits.

Saved SVG now passes through the same DOMPurify/static-SVG restrictions as uploaded SVG. Scripts, event handlers, foreign content, links, animation/URL mutation elements, and stylesheet blocks are removed. Image references are limited to fragment references and supported raster data URLs. A possible SVG animation bypass was investigated, but the original click-based browser proof did **not** reproduce execution; removing SVG animation is additional hardening, not a claimed confirmed exploit.

### UI-04: Sanitization followed by custom-element execution

The former sanitizer removed ordinary event-handler attributes but preserved `<text-widget config='...'>`. When inserted into the live document, `BaseComponent.connectedCallback` parsed that configuration and the widget generated new HTML outside the sanitizer. A malicious tag interpolated into an HTML widget could therefore reach the unsafe renderer, provided the nested widget type was already registered.

The proof used the actual HTML widget, sanitizer, base component, and text widget. The ordinary `<img onerror>` control was blocked even before remediation; the nested custom element was not. The replacement blocks the custom element/configuration route. DOMPurify's documentation specifically cautions that subsequent transformations can undo sanitization, which is why removing active custom components matters here. [DOMPurify documentation](https://github.com/cure53/DOMPurify#is-there-any-foot-gun-potential)

### UI-05: Organisation branding attribute injection

`app-sidebar.ts::escapeHTML` previously used `div.textContent` followed by `div.innerHTML`. That escapes text delimiters but not quotes needed for an HTML attribute. A logo containing `/missing" onerror="...` became an image event handler when the sidebar rendered it. The isolated proof confirmed this.

The default roles give organisation changes only to SystemAdmin, so a default low-privilege user cannot freely plant this field. The server also supports granting `organisations.change` within an accessible organisation; that delegated setup creates a privilege boundary when more privileged users view its branding. This condition limits the default severity and is not a demonstrated unauthenticated attack.

### UI-06: CSV formula injection

`device-list-widget.ts::handleExportXlsx` actually produces CSV. It quoted cells and doubled quotes, but retained leading formula characters from device values and configured headings. Spreadsheet software could interpret those cells as formulas after an operator exported and opened the file.

`ui/src/utils/csv.ts` prefixes potentially active cells before applying CSV quoting, including leading whitespace/control characters and common full-width formula prefixes. Tests verify this behavior and ordinary quoting. No desktop spreadsheet was launched; no claim of code execution in Excel is made. Reimporting or editing exported text in other software can change its interpretation.

### UI-07: Dependency audit

The initial npm report counted **17 affected package entries: 2 critical, 10 high, 4 moderate, and 1 low**. Counts include dependency propagation and are not counts of independently exploitable application bugs. Full before/after JSON is retained under `ui/security-audit/`.

- Vitest/UI 4.0.18 had a critical advisory involving an exposed test API, with additional Windows conditions. This was a development-tool risk, not evidence of production server remote code execution. Updated Vitest/UI to 4.1.11. [Maintainer advisory](https://github.com/vitest-dev/vitest/security/advisories/GHSA-5xrq-8626-4rwp)
- Vite 5.4.21 and nested 7.3.1 had development-server advisories. Updated to 7.3.6 and refreshed affected transitive packages. Network exposure and specific platform/input conditions determine exploitability. [Maintainer advisory](https://github.com/vitejs/vite/security/advisories/GHSA-4w7w-66w2-5vf9)
- ECharts 6.0.0 had a built-in **Lines** series tooltip advisory. The application registers **LineChart** and supplies its own formatter, so that specific upstream path was not established as reachable. Updated to 6.1.0 anyway; the application's separate unsafe custom tooltip is fixed under UI-03. [ECharts advisory](https://github.com/advisories/GHSA-fgmj-fm8m-jvvx)

The final registry-backed audit of the lockfile reports **0 known vulnerabilities**. This is advisory coverage, not proof that dependencies contain no undisclosed bugs. The official SheetJS 0.20.3 distribution is now installed through an integrity-pinned tarball in the lockfile, rather than loaded as a runtime script. Leaflet and CodeMirror are also bundled locally.

### UI-08: Script policy and production surface

The server now sends `script-src 'self'; script-src-attr 'none'` alongside its existing framing/object/base restrictions and a same-origin form policy. It does not permit `unsafe-inline` or `unsafe-eval`. Inline event handlers were replaced by existing/listened event behavior; theme initialization is an external asset under the server's existing `/xact/assets/` route. Same-origin operator-installed plugins remain supported and trusted.

The production build no longer includes the browser test HTML entry, and the Go server no longer serves the `/test/` directory even if stale test files remain on disk. A server regression test checks both startup-asset serving with CSP and the test-runner exclusion.

## Compatibility and deployment

- Map templates retain `${deviceName}`, `${deviceDescription}`, `${tag('path')}`, legacy bare dotted tag paths, literals, basic arithmetic, comparisons, boolean operations, and conditionals. Arbitrary JavaScript, object/property access, and general function calls are intentionally unsupported. Interpolated tag values display as text; static template HTML is sanitized.
- Rich HTML no longer embeds executable custom widgets, frames, forms, or stylesheet blocks. Static SVG artwork may need stylesheet rules moved into ordinary presentation attributes; SVG animations and external image references are intentionally removed.
- CSV cells resembling formulas now export as literal text. Ordinary numeric-looking text beginning with a minus sign is also treated conservatively as text.
- Use Node 20.19+ or 22.12+ for the updated build tooling; existing CI uses Node 22. Rebuild both UI and server so rendering fixes, bundled assets, and the CSP/route changes ship together. No deployment was performed during this work.

## Coverage and verification

The audit inventoried UI source rendering/execution sinks and reviewed authentication/storage, permission gating, API/NATS connections, dashboard loading/imports, rich HTML/SVG/Markdown, maps/charts, administrative forms, reports and exports, visual scripts, plugin loading, and build/dependency configuration. Relevant server authorization/storage routes were traced to identify which actors can supply UI data. Operator-provided plugin JavaScript and shipped manual/icon assets are trusted code/content boundaries, not tenant-uploaded resources.

No additional confirmed authentication bypass, bearer token in URL, cross-origin messaging handler, or browser-controlled arbitrary API origin was established in these paths. Tokens remain in `localStorage`; eliminating script injection and restricting executable sources addresses the demonstrated theft paths without claiming storage is inaccessible to trusted same-origin code.

Verification performed:

- Original UI baseline: **254 tests passed**.
- Updated UI suite: **284 tests passed across 32 files**, including 30 security regression cases.
- TypeScript checking and Vite production build passed.
- Chromium 148 component proofs: nine original execution paths blocked under the pre-fix CSP.
- Production-build browser smoke check: login, Leaflet map, CodeMirror HTML mode, and XLSX export loaded successfully; no uncaught page errors or CSP violations; formula-looking XLSX content remained a string cell.
- Targeted Go security-header/static-asset tests cover the actual server middleware and routing.
- Final npm advisory audit: **zero known vulnerabilities**.

Browser checks used isolated loopback fixtures and blocked external requests. They do not replace a deployed-system penetration test, real NATS/device integration testing, or cross-browser testing on Firefox/Safari. The SVG animation hypothesis above was not reproduced. These limits are separate from the confirmed and fixed execution paths.

Re-run from `ui/` with dependencies installed:

```sh
npm run test:run
npm run build
npm audit --package-lock-only --ignore-scripts
node security-audit/reproduce.mjs
node security-audit/smoke.mjs dist
```

The browser scripts use Playwright Chromium. Set `AUDIT_CHROMIUM_EXECUTABLE` if using a separately installed compatible Chromium. The reproduction script intentionally retains the original CSP and exits unsuccessfully if any execution proof fires. It uses synthetic session data only. Evidence: [before](ui/security-audit/browser-before.json), [after](ui/security-audit/browser-after.json), [production smoke](ui/security-audit/browser-smoke.json), [dependency audit before](ui/security-audit/npm-before.json), [dependency audit after](ui/security-audit/npm-after.json).
