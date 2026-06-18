import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

describe('vite dev proxy', () => {
  it('proxies the health endpoint used by the footer', () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const config = readFileSync(resolve(here, '../vite.config.ts'), 'utf8');

    expect(config).toMatch(/'\/xact\/health':\s*{\s*target: 'http:\/\/localhost:8080',\s*changeOrigin: true,\s*}/);
    expect(config).toMatch(/'\/xact\/openapi\.json':\s*{\s*target: 'http:\/\/localhost:8080',\s*changeOrigin: true,\s*}/);
  });
});
