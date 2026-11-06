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

// 2) 控件库导出必须与 registry 对应；一个控件可以注册多种样式；ID 必须唯一且支持 <控件>/<样式>。
const exported = new Set();
const indexed = parse(path.join(uiRoot, 'index.ts'));
indexed.forEachChild((node) => {
  if (ts.isExportDeclaration(node) && node.exportClause && ts.isNamedExports(node.exportClause)) {
    for (const element of node.exportClause.elements) {
      if (!element.isTypeOnly) exported.add(element.name.text);
    }
  }
});

const registryFile = path.join(uiRoot, 'registry.tsx');
const registry = parse(registryFile);
const categories = new Map();
const registeredComponents = new Set();
const ids = new Map();

// 2.1 读取分类
registry.forEachChild((node) => {
  if (!ts.isVariableStatement(node)) return;
  for (const declaration of node.declarationList.declarations) {
    let initializer = declaration.initializer;
    while (
      initializer &&
      (ts.isAsExpression(initializer) || ts.isParenthesizedExpression(initializer))
    ) {
      initializer = initializer.expression;
    }
    if (
      ts.isIdentifier(declaration.name) &&
      declaration.name.text === 'controlCategories' &&
      initializer &&
      ts.isArrayLiteralExpression(initializer)
    ) {
      for (const element of initializer.elements) {
        if (!ts.isObjectLiteralExpression(element)) continue;
        const idProperty = element.properties.find(
          (property) => ts.isPropertyAssignment(property) && property.name.getText() === 'id',
        );
        if (
          idProperty &&
          ts.isPropertyAssignment(idProperty) &&
          ts.isStringLiteral(idProperty.initializer)
        ) {
          const id = idProperty.initializer.text;
          categories.set(id, (categories.get(id) ?? 0) + 1);
          if (!/^[a-z][a-z0-9-]*$/.test(id)) {
            errors.push(`分类 ID "${id}" 不是 kebab-case`);
          }
        }
      }
    }
  }
});
if (categories.size === 0) errors.push('registry 缺少 controlCategories 分类定义');

// 2.2 读取展示型控件名单：这些只渲染、不接受交互，接线检查跳过它们。
// 名单放在 registry 里而不是脚本里，是为了让它和控件定义出现在同一个 diff 里。
const presentational = new Set();
registry.forEachChild((node) => {
  if (!ts.isVariableStatement(node)) return;
  for (const declaration of node.declarationList.declarations) {
    let initializer = declaration.initializer;
    while (
      initializer &&
      (ts.isAsExpression(initializer) || ts.isParenthesizedExpression(initializer))
    ) {
      initializer = initializer.expression;
    }
    if (
      ts.isIdentifier(declaration.name) &&
      declaration.name.text === 'presentationalControls' &&
      initializer &&
      ts.isArrayLiteralExpression(initializer)
    ) {
      for (const element of initializer.elements) {
        if (ts.isStringLiteral(element)) presentational.add(element.text);
      }
    }
  }
});

const entries = [];
const visitRegistry = (node) => {
  if (ts.isObjectLiteralExpression(node)) {
    const props = new Map();
    for (const property of node.properties) {
      if (ts.isPropertyAssignment(property) && ts.isIdentifier(property.name)) {
        props.set(property.name.text, property.initializer);
      }
    }
    const componentValue = props.get('component');
    if (componentValue && ts.isStringLiteral(componentValue)) {
      entries.push({ node, props });
    }
  }
  ts.forEachChild(node, visitRegistry);
};
visitRegistry(registry);

for (const { node, props } of entries) {
  const nameValue = props.get('name');
  const idValue = props.get('id');
  const componentValue = props.get('component');
  const categoryValue = props.get('category');

  const name =
    nameValue && ts.isStringLiteral(nameValue)
      ? nameValue.text
      : `(line ${lineOf(registry, node)})`;
  if (!nameValue || !ts.isStringLiteral(nameValue) || nameValue.text.trim() === '') {
    errors.push(`控件 ${name} 缺少字符串字面量 name`);
  }
  if (!componentValue || !ts.isStringLiteral(componentValue)) {
    errors.push(`控件 ${name} 缺少字符串字面量 component`);
    continue;
  }
  const component = componentValue.text;
  registeredComponents.add(component);

  if (!idValue || !ts.isStringLiteral(idValue) || idValue.text.trim() === '') {
    errors.push(`控件 ${name} 缺少字符串字面量 id`);
  } else {
    const id = idValue.text;
    if (!/^[a-z][a-z0-9-]*(?:\/[a-z0-9-]+)*$/.test(id)) {
      errors.push(`控件 ${name} 的注册名 "${id}" 不合法（应为 <控件>/<样式> 形式）`);
    }
    ids.set(id, (ids.get(id) ?? 0) + 1);
  }

  if (!categoryValue || !ts.isStringLiteral(categoryValue)) {
    errors.push(`控件 ${name} 缺少字符串字面量 category`);
  } else if (!categories.has(categoryValue.text)) {
    errors.push(`控件 ${name} 的分类 "${categoryValue.text}" 未在 controlCategories 中定义`);
  }
}

for (const name of exported) {
  if (!registeredComponents.has(name)) {
    errors.push(`控件库导出 ${name} 未在 Dev Room registry 登记`);
  }
}
for (const component of registeredComponents) {
  if (!exported.has(component)) {
    errors.push(`Dev Room registry 引用了未导出的控件 ${component}`);
  }
}
for (const name of presentational) {
  if (!registeredComponents.has(name)) {
    errors.push(`presentationalControls 里的 "${name}" 不是已注册的控件——拼错了？`);
  }
}
for (const [id, count] of ids) {
  if (count > 1) errors.push(`Dev Room 控件注册名重复: ${id}`);
}
for (const [id, count] of categories) {
  if (count > 1) errors.push(`分类 ID 重复: ${id}`);
}

// 3) Dev Room 必须把注册名渲染到 DOM（data-control-id），供用户/agent 精确定位。
if (
  !/\bdata-control-id\b/.test(fs.readFileSync(path.join(devRoomRoot, 'DevRoomPage.tsx'), 'utf8'))
) {
  errors.push('DevRoomPage 没有渲染 data-control-id，Dev Room 控件无法被精确定位');
}

// 4) Dev Room 之外的**交互型**控件必须接线。
//
// 「接线」= 收到一个 React 事件回调（`onXxx`）或一个导航目标（`href` / `to` / `type=submit`）。
// 不按固定属性名单判，因为控件的回调名本来就应该由门禁决定（`onPageChange`、`onRetry`
// 都合理）；按 `on[A-Z]` 判定既覆盖了全部 React 事件约定，也不会放行一个纯字面量属性。
//
// 展示型控件（`presentationalControls`）跳过这一条：Skeleton 不响应用户，EmptyState 的动作
// 是可选的。它们没有「接线」这个概念。
const interactionProps = new Set(['href', 'to']);
const isHandlerProp = (name) => /^on[A-Z]/.test(name);

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
      if (ts.isIdentifier(tag) && controlImports.has(tag.text) && !presentational.has(tag.text)) {
        const wired = node.attributes.properties.some((attribute) => {
          if (!ts.isJsxAttribute(attribute)) return false;
          const name = attribute.name.getText(sf);
          if (interactionProps.has(name) || isHandlerProp(name)) return true;
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

// 5) 页面里不许出现原生交互控件。
//
// AGENTS 写的是「页面只能从控件库导入控件」，而第 4 条只查「从控件库导入的控件有没有接线」：
// 原生 `<button>` / `<textarea>` 一个都不导入，于是整条规则对它们完全隐形。三处裸
// `<textarea>` 与七处裸 `<button>` 就是这么长出来的——门禁全绿，页面里却各有各的边框色。
//
// 扫描范围是**页面层与页面级组件**：`src/routes` 与 `src/components`（除控件库 ui/、
// Dev Room 与测试）。只扫 routes 曾经放过一次：`src/components/model-picker.tsx`
// 里就能写裸 `<button>` 而门禁全绿——AGENTS 的原话是「页面（含页面级组件）」，
// 而页面级组件恰好住在 components/ 下。
const nativeControls = new Set(['button', 'textarea', 'input', 'select']);
const routesRoot = path.join(srcRoot, 'routes');
const componentsRoot = path.join(srcRoot, 'components');
// 控件库自己要用原生元素（它就是把原生控件包起来的），Dev Room 是演示场。
//
// Dev Room 的路径必须用 devRoomRoot：它住在 `src/dev-room`，不是 `src/components/dev-room`。
// 上一版的写法拼出了后者的路径，那条豁免从加进去那天起就没命中过——而它不命中
// 也不报错，于是「豁免存在」与「豁免生效」是两件事。
const nativeExempt = [uiRoot, devRoomRoot];
for (const file of sourceFiles) {
  const inScope =
    isInside(file, routesRoot) ||
    (isInside(file, componentsRoot) && !nativeExempt.some((dir) => isInside(file, dir)));
  if (!inScope) continue;
  // 测试里的合成触发器不是页面控件（例如「用 useNavigate 的按钮」验证 URL 变化后输入框跟随），
  // 它不出现在用户看到的界面上，套控件库只会让测试依赖一件与它无关的东西。
  if (/\.test\.(ts|tsx)$/.test(file)) continue;
  const sf = parse(file);
  const checkNative = (node) => {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      const tag = node.tagName;
      if (ts.isIdentifier(tag) && nativeControls.has(tag.text)) {
        errors.push(
          `${rel(file)}:${lineOf(sf, node)} 用了原生 <${tag.text}>，请改用控件库里的对应控件（见 AGENTS「控件库」）`,
        );
      }
    }
    ts.forEachChild(node, checkNative);
  };
  checkNative(sf);
}

if (errors.length > 0) {
  console.error(`ui-contract failed (${errors.length}):`);
  for (const error of errors) console.error(`  - ${error}`);
  process.exit(1);
}

console.log(
  `ui-contract ok: ${exported.size} components, ${ids.size} registrations, ${categories.size} categories, ${sourceFiles.length} files`,
);
