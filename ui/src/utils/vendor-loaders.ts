// Executable dependencies are versioned in the lockfile and served locally.
let codeMirrorReady: Promise<void> | null = null;
export function loadCodeMirror(): Promise<void> {
  if ((window as any).CodeMirror) return Promise.resolve();
  return codeMirrorReady ??= (async () => {
    const { default: CodeMirror } = await import('codemirror');
    await Promise.all([
      import('codemirror/lib/codemirror.css'),
      import('codemirror/theme/dracula.css'),
      import('codemirror/mode/xml/xml.js'),
      import('codemirror/mode/javascript/javascript.js'),
      import('codemirror/mode/css/css.js'),
    ]);
    await import('codemirror/mode/htmlmixed/htmlmixed.js');
    (window as any).CodeMirror = CodeMirror;
  })().catch(error => { codeMirrorReady = null; throw error; });
}

let leafletReady: Promise<void> | null = null;
export function loadLeaflet(): Promise<void> {
  if ((window as any).L) return Promise.resolve();
  return leafletReady ??= (async () => {
    const leaflet = await import('leaflet');
    await import('leaflet/dist/leaflet.css');
    (window as any).L = leaflet;
  })().catch(error => { leafletReady = null; throw error; });
}
