"""生成 / 校验 OpenAPI 契约快照。

用法（在 backend/ 下）：
    python -m scripts.dump_openapi           # 重新生成快照
    python -m scripts.dump_openapi --check   # 只校验，有差异时退出码 1

导入 app.main 会创建 storage/media 目录，因此这里先把 DATABASE_URL / STORAGE_DIR
指到临时目录，避免在仓库里留下运行期文件。
"""

from __future__ import annotations

import argparse
import os
import sys
import tempfile
from pathlib import Path

_BACKEND = Path(__file__).resolve().parents[1]
if str(_BACKEND) not in sys.path:
    sys.path.insert(0, str(_BACKEND))

_tmp = Path(tempfile.mkdtemp(prefix="dsh_openapi_"))
os.environ.setdefault("DATABASE_URL", f"sqlite:///{(_tmp / 'openapi.db').as_posix()}")
os.environ.setdefault("STORAGE_DIR", (_tmp / "storage").as_posix())
os.environ.setdefault("OSS_ENABLED", "false")

from scripts.openapi_contract import (  # noqa: E402
    SNAPSHOT_PATH,
    diff_summary,
    dumps,
    load_snapshot,
    project_schema,
    write_snapshot,
)


def main() -> int:
    parser = argparse.ArgumentParser(description="生成或校验 OpenAPI 契约快照")
    parser.add_argument("--check", action="store_true", help="只校验，不写文件")
    parser.add_argument("--path", type=Path, default=SNAPSHOT_PATH, help="快照路径")
    args = parser.parse_args()

    from app.main import app  # noqa: PLC0415 —— 必须在环境变量就绪后导入

    projection = project_schema(app.openapi())

    if not args.check:
        write_snapshot(projection, args.path)
        print(f"已写入 {args.path}")
        return 0

    if not args.path.exists():
        print(f"快照不存在：{args.path}\n请先运行 python -m scripts.dump_openapi")
        return 1

    expected = load_snapshot(args.path)
    if dumps(expected) == dumps(projection):
        print(f"契约一致：{args.path}")
        return 0

    print("契约与快照不一致：")
    print(diff_summary(expected, projection))
    print("\n若确为预期变更，运行 python -m scripts.dump_openapi 更新快照并随 PR 提交。")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
