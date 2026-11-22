import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';

const ROOTS = ['src/routes', 'src/features', 'src/components'];

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (path.replaceAll('\\', '/').includes('components/ui')) return [];
      return files(path);
    }
    return entry.name.endsWith('.tsx') ? [path] : [];
  });
}

describe('产品页面暗色约束', () => {
  test('产品页面不包含 dark: 变体', () => {
    const offenders = ROOTS.flatMap((root) => files(root))
      .filter((path) => readFileSync(path, 'utf8').includes('dark:'))
      .map((path) => path.replaceAll('\\', '/'));
    expect(offenders).toEqual([]);
  });
});
