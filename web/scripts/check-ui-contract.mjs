#!/usr/bin/env node
// 硬约束：页面只能从控件库取控件；控件库导出必须与 Dev Room registry 一致且带稳定 ID；
// Dev Room 外的控件必须接线。
// shortcut: 接线检查按 JSX 属性做启发式判断，出现误报再升级为带类型的 ESLint 规则。
import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';

const webRoot = path.resolve(import.meta.dirname, '..');
const srcRoot = path.join(webRoot, 'src');
const uiRoot = path.join(srcRoot, 'components', 'ui');
const devRoomRoot = path.join(srcRoot, 'dev-room');
const testRoot = path.join(srcRoot, 'test');
const errors = [];

const rel = (file) => path.relative(webRoot, file).split(path.sep).join('/');
const isInside = (file, dir) => {
  const relative = path.relative(dir, file);
  return relative !== '' && !relative.startsWith('..') && !path.isAbsolute(relative);
};
const parse = (file) =>
  ts.createSourceFile(
    file,
    fs.readFileSync(file, 'utf8'),
    ts.ScriptTarget.Latest,
    true,
    file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
const lineOf = (sf, node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;

const sourceFiles = fs
  .readdirSync(srcRoot, { recursive: true })
  .filter((entry) => typeof entry === 'string' && /\.(ts|tsx)$/.test(entry))
  .map((entry) => path.join(srcRoot, entry));

// 1) Ring UI 只允许出现在控件库内部。
const isRingUI = (spec) =>
  spec === '@jetbrains/ring-ui-built' ||
  spec.startsWith('@jetbrains/ring-ui-built/') ||
  spec === '@jetbrains/ring-ui' ||
  spec.startsWith('@jetbrains/ring-ui/');

for (const file of sourceFiles) {
  if (isInside(file, uiRoot)) continue;
  const sf = parse(file);
  const visit = (node) => {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      if (isRingUI(node.moduleSpecifier.text)) {
        errors.push(
          `${rel(file)}:${lineOf(sf, node)} 页面不得直接 import Ring UI，请从 @/components/ui 取控件`,
        );
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// 2) 控件库导出清单必须与 Dev Room registry 完全一致，且每个控件有唯一稳定 ID。
const exported = new Set();
const indexed = parse(path.join(uiRoot, 'index.ts'));
indexed.forEachChild((node) => {
  if (ts.isExportDeclaration(node) && node.exportClause && ts.isNamedExports(node.exportClause)) {
    for (const element of node.exportClause.elements) {
      if (!element.isTypeOnly) exported.add(element.name.text);
    }
  }
});

const registered = new Map();
const ids = new Map();
const registry = parse(path.join(uiRoot, 'registry.tsx'));
const visitRegistry = (node) => {
  if (ts.isObjectLiteralExpression(node)) {
    const props = new Map();
    for (const property of node.properties) {
      if (ts.isPropertyAssignment(property) && ts.isIdentifier(property.name)) {
        props.set(property.name.text, property.initializer);
      }
    }
    const nameValue = props.get('name');
    const idValue = props.get('id');
    if (nameValue && ts.isStringLiteral(nameValue)) {
      const name = nameValue.text;
      registered.set(name, (registered.get(name) ?? 0) + 1);
      if (!idValue || !ts.isStringLiteral(idValue) || idValue.text.trim() === '') {
        errors.push(`控件 ${name} 缺少字符串字面量 id（Dev Room 需要稳定控件 ID）`);
      } else {
        const id = idValue.text;
        if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(id)) {
          errors.push(`控件 ${name} 的 id "${id}" 不是 kebab-case`);
        }
        ids.set(id, (ids.get(id) ?? 0) + 1);
      }
    }
  }
  ts.forEachChild(node, visitRegistry);
};
visitRegistry(registry);

for (const name of exported) {
  if (!registered.has(name)) errors.push(`控件库导出 ${name} 未在 Dev Room registry 登记`);
}
for (const [name, count] of registered) {
  if (!exported.has(name)) errors.push(`Dev Room registry 登记了 ${name}，但控件库未导出`);
  if (count > 1) errors.push(`Dev Room registry 重复登记了 ${name}`);
}
for (const [id, count] of ids) {
  if (count > 1) errors.push(`Dev Room 控件 ID 重复: ${id}`);
}

// Dev Room 必须把 ID 渲染到 DOM（data-control-id），供用户/agent 精确定位。
if (
  !/\bdata-control-id\b/.test(fs.readFileSync(path.join(devRoomRoot, 'DevRoomPage.tsx'), 'utf8'))
) {
  errors.push('DevRoomPage 没有渲染 data-control-id，Dev Room 控件无法被精确定位');
}

// 3) Dev Room 之外的控件必须接线（至少一个真实交互属性）。
const interactionProps = new Set([
  'onClick',
  'onChange',
  'onSubmit',
  'onSelect',
  'onCheckedChange',
  'onOpenChange',
  'href',
  'to',
]);

for (const file of sourceFiles) {
  if (isInside(file, uiRoot) || isInside(file, devRoomRoot) || isInside(file, testRoot)) continue;
  const sf = parse(file);
  const controlImports = new Set();

  const collectImports = (node) => {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      const spec = node.moduleSpecifier.text;
      if (spec === '@/components/ui' || /(^|\/)components\/ui($|\/)/.test(spec)) {
        const named = node.importClause?.namedBindings;
        if (named && ts.isNamedImports(named)) {
          for (const element of named.elements) controlImports.add(element.name.text);
        }
      }
    }
    ts.forEachChild(node, collectImports);
  };
  collectImports(sf);
  if (controlImports.size === 0) continue;

  const checkJsx = (node) => {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      const tag = node.tagName;
      if (ts.isIdentifier(tag) && controlImports.has(tag.text)) {
        const wired = node.attributes.properties.some((attribute) => {
          if (!ts.isJsxAttribute(attribute)) return false;
          const name = attribute.name.getText(sf);
          if (interactionProps.has(name)) return true;
          return (
            name === 'type' &&
            attribute.initializer &&
            ts.isStringLiteral(attribute.initializer) &&
            attribute.initializer.text === 'submit'
          );
        });
        if (!wired) {
          errors.push(
            `${rel(file)}:${lineOf(sf, node)} <${tag.text}> 没有接线（需要 onClick/onChange/onSubmit/href/to/type=submit 之一）`,
          );
        }
      }
    }
    ts.forEachChild(node, checkJsx);
  };
  checkJsx(sf);
}

if (errors.length > 0) {
  console.error(`ui-contract failed (${errors.length}):`);
  for (const error of errors) console.error(`  - ${error}`);
  process.exit(1);
}

console.log(
  `ui-contract ok: ${exported.size} controls, ${ids.size} ids, ${sourceFiles.length} source files`,
);
