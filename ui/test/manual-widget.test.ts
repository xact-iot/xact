import { afterEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import '../src/dashboards/widgets/manual-widget';

async function waitFor(assertion: () => void): Promise<void> {
  const deadline = Date.now() + 1000;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      assertion();
      return;
    } catch (err) {
      lastError = err;
      await new Promise(resolve => setTimeout(resolve, 10));
    }
  }
  throw lastError;
}

describe('manual-widget', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    document.body.innerHTML = '';
  });

  it('loads the manifest and initial chapter once when config is set before attach', async () => {
    const manifest = {
      title: 'Manual',
      chapters: [
        { id: 'getting-started', file: '01-getting-started.md', title: 'Getting Started' },
      ],
    };
    const fetchMock = vi.fn(async (url: string) => {
      if (url.endsWith('/manifest.json')) {
        return {
          ok: true,
          json: async () => manifest,
        } as Response;
      }
      if (url.endsWith('/01-getting-started.md')) {
        return {
          ok: true,
          text: async () => '# Getting Started\n\nWelcome.',
        } as Response;
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    const widget = document.createElement('manual-widget') as any;
    widget.setConfig({ chapter: '' });
    document.body.appendChild(widget);

    await waitFor(() => {
      expect(widget.querySelector('.mw-md')?.textContent).toContain('Welcome.');
    });

    const urls = fetchMock.mock.calls.map(call => call[0]);
    expect(urls.filter(url => String(url).endsWith('/manifest.json'))).toHaveLength(1);
    expect(urls.filter(url => String(url).endsWith('/01-getting-started.md'))).toHaveLength(1);
  });

  it('lists Automation immediately after Scheduler with a complete chapter', () => {
    const manifest = JSON.parse(readFileSync('public/manual/manifest.json', 'utf8'));
    const schedulerIndex = manifest.chapters.findIndex((chapter: { id: string }) => chapter.id === 'scheduler');

    expect(schedulerIndex).toBeGreaterThanOrEqual(0);
    expect(manifest.chapters[schedulerIndex + 1]).toEqual({
      id: 'automation', file: '13-automation.md', title: 'Automation',
    });

    const automation = readFileSync('public/manual/13-automation.md', 'utf8');
    expect(automation).toContain('# Automation');
    expect(automation).toContain('## Build and Test a First Script');
    expect(automation).toContain('## Node Reference');
    expect(automation).toContain('## Wildcard Tag Triggers and Script Instances');
  });
});
