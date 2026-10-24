"""OpenAPI 契约快照的投影与读写（供 dump 脚本与契约测试共用）。

为什么是投影而不是整份 schema：本仓库依赖未锁小版本（见 requirements.txt），
FastAPI / Pydantic 的补丁升级会改变 schema 的内部写法（anyOf 顺序、const/enum 表示、
default 是否回填），整份 JSON 对拍会把「依赖升级」误报成「接口变更」。

投影保留跨版本稳定的契约面：
- 路径 / 方法 / operationId / tag / 鉴权 / 参数（名、位置、必填、类型）
- 请求与响应的媒体类型 + schema 引用名（响应文案不参与对拍）
- 组件模型的字段名、类型与 required 列表（能抓到字段增删改）

字段的约束细节（minLength、pattern 等）由各端点测试与 Pydantic 模型本身保证。
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

SNAPSHOT_PATH = Path(__file__).resolve().parents[1] / "tests" / "snapshots" / "openapi-contract.json"


def _describe(schema: Any) -> str:
    """把一个 JSON Schema 片段压成稳定字符串：引用名优先，其次类型/枚举/数组。"""
    if not isinstance(schema, dict):
        return "unknown"
    if "$ref" in schema:
        return str(schema["$ref"]).rsplit("/", 1)[-1]
    if "enum" in schema and isinstance(schema["enum"], list):
        return "enum(" + "|".join(sorted(str(v) for v in schema["enum"])) + ")"
    if "const" in schema:
        return f"const({schema['const']})"
    for key in ("anyOf", "oneOf", "allOf"):
        if key in schema and isinstance(schema[key], list):
            parts = sorted({_describe(item) for item in schema[key]})
            return f"{key}(" + ",".join(parts) + ")"
    kind = schema.get("type")
    if kind == "array":
        return "array<" + _describe(schema.get("items")) + ">"
    if isinstance(kind, list):
        return "|".join(sorted(str(k) for k in kind))
    return str(kind or "unknown")


def _content(schema: Any) -> dict[str, str]:
    if not isinstance(schema, dict):
        return {}
    content = schema.get("content") or {}
    return {media: _describe(media_schema.get("schema")) for media, media_schema in sorted(content.items())}


def _parameters(op: dict[str, Any]) -> list[dict[str, Any]]:
    out = []
    for param in op.get("parameters") or []:
        out.append({
            "name": param.get("name"),
            "in": param.get("in"),
            "required": bool(param.get("required")),
            "schema": _describe(param.get("schema")),
        })
    return sorted(out, key=lambda p: (str(p["in"]), str(p["name"])))


def _model_schema(schema: Any) -> dict[str, Any]:
    """组件模型只留字段名/类型/必填：能抓到字段增删改，又不受 Pydantic 版本写法影响。"""
    if not isinstance(schema, dict):
        return {}
    props = schema.get("properties") or {}
    return {
        "required": sorted(schema.get("required") or []),
        "properties": {name: _describe(value) for name, value in sorted(props.items())},
    }


def project_schema(schema: dict[str, Any]) -> dict[str, Any]:
    """把 app.openapi() 投影成与依赖小版本无关的契约快照。"""
    paths: dict[str, dict[str, Any]] = {}
    for path in sorted(schema.get("paths") or {}):
        methods: dict[str, Any] = {}
        for method in sorted(schema["paths"][path]):
            op = schema["paths"][path][method]
            if not isinstance(op, dict):
                continue
            request = op.get("requestBody") or {}
            methods[method] = {
                "operationId": op.get("operationId"),
                "tags": sorted(op.get("tags") or []),
                "security": op.get("security") or [],
                "deprecated": bool(op.get("deprecated")),
                "parameters": _parameters(op),
                "requestBody": {
                    "required": bool(request.get("required")),
                    "content": _content(request),
                } if request else None,
                # 只记状态码与媒体类型/schema 引用：响应描述文案随实现改写而变，不属于契约
                "responses": {
                    str(status): _content(resp)
                    for status, resp in sorted((op.get("responses") or {}).items())
                },
            }
        paths[path] = methods

    components = schema.get("components") or {}
    return {
        "info": {
            "title": (schema.get("info") or {}).get("title"),
            "version": (schema.get("info") or {}).get("version"),
        },
        "paths": paths,
        "schemas": {
            name: _model_schema(value)
            for name, value in sorted((components.get("schemas") or {}).items())
        },
        "securitySchemes": sorted((components.get("securitySchemes") or {}).keys()),
    }


def dumps(projection: dict[str, Any]) -> str:
    return json.dumps(projection, ensure_ascii=False, indent=2, sort_keys=True) + "\n"


def load_snapshot(path: Path = SNAPSHOT_PATH) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def write_snapshot(projection: dict[str, Any], path: Path = SNAPSHOT_PATH) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(dumps(projection), encoding="utf-8")


def diff_summary(expected: Any, actual: Any, prefix: str = "") -> str:
    """返回第一处差异的可读描述（快照=expected，当前=actual）。"""
    if type(expected) is not type(actual):
        return f"{prefix or '<root>'}: 类型变化 {type(expected).__name__} -> {type(actual).__name__}"
    if isinstance(expected, dict):
        for key in sorted(set(expected) | set(actual)):
            if key not in expected:
                return f"{prefix}/{key}: 新增 {actual[key]!r}"
            if key not in actual:
                return f"{prefix}/{key}: 移除 {expected[key]!r}"
            found = diff_summary(expected[key], actual[key], f"{prefix}/{key}")
            if found:
                return found
        return ""
    if isinstance(expected, list):
        if expected != actual:
            return f"{prefix}: 列表变化 {expected!r} -> {actual!r}"
        return ""
    if expected != actual:
        return f"{prefix}: {expected!r} -> {actual!r}"
    return ""
