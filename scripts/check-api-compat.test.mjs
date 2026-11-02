import { test } from 'node:test';
import assert from 'node:assert/strict';
import { extractSurface, diffSurface } from './check-api-compat.mjs';

const SRC = `package api

const (
	Ok   HealthzStatus = "ok"
	Fail HealthzStatus = "fail"
)

type Healthz struct {
	Status HealthzStatus \`json:"status"\`
}

type Item struct {
	Id    int     \`json:"id"\`
	Title *string \`json:"title"\`
}

type ServerInterface interface {
	// 健康检查
	// (GET /healthz)
	GetHealthz(w http.ResponseWriter, r *http.Request)
	// 详情
	// (GET /items/{slug})
	GetItem(w http.ResponseWriter, r *http.Request, slug string)
	// 列表
	// (GET /items)
	ListItems(w http.ResponseWriter, r *http.Request, params ListItemsParams)
}
`;

test('抽取：枚举 / 模型 / 三个端点（含路径参数那种）', () => {
  const s = extractSurface(SRC);
  assert.deepEqual(s.enums.HealthzStatus, ['fail', 'ok']);
  assert.deepEqual(Object.keys(s.models.Item), ['id', 'title']);
  assert.equal(s.models.Item.title.optional, true);
  assert.equal(s.models.Item.id.optional, false);
  assert.deepEqual(Object.keys(s.endpoints).sort(), ['GET /healthz', 'GET /items', 'GET /items/{slug}']);
  assert.equal(s.endpoints['GET /items'].params, 'ListItemsParams');
  assert.equal(s.endpoints['GET /items/{slug}'].pathParams, 1);
  assert.equal(s.endpoints['GET /healthz'].params, null);
});

test('删字段 = 破坏性', () => {
  const base = extractSurface(SRC);
  const cur = extractSurface(SRC.replace('\tTitle *string `json:"title"`\n', ''));
  const breaking = diffSurface(base, cur).filter((f) => f.level === 'breaking');
  assert.equal(breaking.length, 1);
  assert.match(breaking[0].message, /字段被移除/);
});

test('改字段类型 = 破坏性', () => {
  const base = extractSurface(SRC);
  const cur = extractSurface(SRC.replace('Id    int     `json:"id"`', 'Id    string  `json:"id"`'));
  const breaking = diffSurface(base, cur).filter((f) => f.level === 'breaking');
  assert.equal(breaking.length, 1);
  assert.match(breaking[0].message, /类型从 int 改成 string/);
});

test('可空变必填 = 破坏性；必填变可空 = 提示', () => {
  const base = extractSurface(SRC);
  const tight = extractSurface(SRC.replace('Title *string `json:"title"`', 'Title string  `json:"title"`'));
  assert.equal(diffSurface(base, tight).filter((f) => f.level === 'breaking').length, 1);

  const loose = extractSurface(SRC.replace('Id    int     `json:"id"`', 'Id    *int    `json:"id"`'));
  const levels = diffSurface(base, loose).map((f) => f.level);
  assert.ok(levels.includes('warn'));
  assert.ok(!levels.includes('breaking'));
});

test('删枚举取值 = 破坏性；加取值 = 提示', () => {
  const base = extractSurface(SRC);
  const removed = extractSurface(SRC.replace('\tFail HealthzStatus = "fail"\n', ''));
  assert.equal(diffSurface(base, removed).filter((f) => f.level === 'breaking').length, 1);

  const added = extractSurface(SRC.replace('\tFail HealthzStatus = "fail"\n', '\tFail HealthzStatus = "fail"\n\tDown HealthzStatus = "down"\n'));
  const findings = diffSurface(base, added);
  assert.ok(findings.some((f) => f.level === 'warn' && /新增枚举取值/.test(f.message)));
  assert.ok(!findings.some((f) => f.level === 'breaking'));
});

test('删端点 = 破坏性；加端点不算', () => {
  const base = extractSurface(SRC);
  const cur = extractSurface(SRC.replace('\t// 健康检查\n\t// (GET /healthz)\n\tGetHealthz(w http.ResponseWriter, r *http.Request)\n', ''));
  const findings = diffSurface(base, cur);
  assert.ok(findings.some((f) => f.level === 'breaking' && /端点被移除/.test(f.message)));
  assert.ok(!findings.some((f) => f.level === 'breaking' && !/端点被移除/.test(f.message)));
});

test('删模型 = 破坏性', () => {
  const base = extractSurface(SRC);
  const cur = extractSurface(SRC.replace(/type Item struct \{[\s\S]*?\n\}\n/, ''));
  const breaking = diffSurface(base, cur).filter((f) => f.level === 'breaking');
  assert.equal(breaking.length, 1);
  assert.match(breaking[0].message, /模型被移除/);
});

test('原样不动 = 零判定', () => {
  assert.deepEqual(diffSurface(extractSurface(SRC), extractSurface(SRC)), []);
});
