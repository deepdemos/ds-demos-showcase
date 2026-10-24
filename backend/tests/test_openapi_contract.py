"""OpenAPI 契约快照：用快照钉住对外契约面。

契约面 = 路径 + 方法 + operationId + 鉴权 + 参数 + 请求/响应媒体类型与 schema 引用。
P1/P2 的物理分包与 `demos.py` 拆分必须让本测试保持通过：搬文件、改 import 不应改变契约。
"""

from __future__ import annotations

import re

import pytest

from app.main import app
from scripts.openapi_contract import (
    SNAPSHOT_PATH,
    diff_summary,
    dumps,
    load_snapshot,
    project_schema,
)

# Starlette 的 `{param:path}` 在 OpenAPI 里写作 `{param}`，对拍前统一去掉转换器
_CONVERTER = re.compile(r"\{([a-zA-Z_][a-zA-Z0-9_]*):[a-zA-Z_]+\}")


def test_openapi_contract_matches_snapshot():
    assert SNAPSHOT_PATH.exists(), (
        f"缺少契约快照 {SNAPSHOT_PATH}；请运行 python -m scripts.dump_openapi 生成"
    )
    expected = load_snapshot()
    actual = project_schema(app.openapi())
    if dumps(expected) != dumps(actual):
        pytest.fail(
            "OpenAPI 契约与快照不一致：\n"
            f"{diff_summary(expected, actual)}\n\n"
            "若这是有意的契约变更，运行 python -m scripts.dump_openapi 更新快照并随 PR 提交；"
            "若是重构误伤，请恢复接口签名。"
        )


def test_snapshot_covers_all_registered_routes():
    """快照不能悄悄少记路由：新增端点必须出现在契约里。"""
    snapshot = load_snapshot()
    documented = {
        (path, method)
        for path, methods in snapshot["paths"].items()
        for method in methods
    }
    registered = {
        (_CONVERTER.sub(r"{\1}", route.path), method.lower())
        for route in app.routes
        if getattr(route, "methods", None)
        for method in route.methods
        if method.lower() not in {"head", "options"}
        and route.path.startswith(("/api/", "/preview/", "/media/"))
    }
    missing = sorted(registered - documented)
    assert not missing, f"以下路由未进入 OpenAPI 快照：{missing}"


def test_projected_schema_detects_field_changes():
    """投影必须能抓到字段级变更（否则契约测试只是摆设）。"""
    schema = {
        "info": {"title": "t", "version": "1"},
        "paths": {},
        "components": {
            "schemas": {
                "Thing": {
                    "type": "object",
                    "required": ["id"],
                    "properties": {"id": {"type": "integer"}, "name": {"type": "string"}},
                },
            },
        },
    }
    base = project_schema(schema)
    changed = {
        **schema,
        "components": {
            "schemas": {
                "Thing": {
                    "type": "object",
                    "required": ["id"],
                    "properties": {"id": {"type": "integer"}},
                },
            },
        },
    }
    assert base["schemas"]["Thing"]["properties"] != project_schema(changed)["schemas"]["Thing"]["properties"]
