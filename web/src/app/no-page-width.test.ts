/// <reference types="node" />
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';

const ROUTES = 'src/routes';

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return files(path);
    return entry.name.endsWith('.tsx') ? [path] : [];
  });
}

describe('页面宽度声明', () => {
  test('页面不再手写 width', () => {
    const offenders = files(ROUTES)
      .filter((path) => /<Page\b[^>]*\bwidth\s*=/.test(readFileSync(path, 'utf8')))
      .map((path) => path.replaceAll('\\', '/'));
    expect(offenders).toEqual([]);
  });
});
