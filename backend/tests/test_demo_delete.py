"""删除作品的外键清理（2026-09 生产 500 事故的回归测试）。

事故现场（服务器日志）：
    DELETE /api/v1/demos/demo-08d1b38d → 500
    sqlite3.IntegrityError: FOREIGN KEY constraint failed
    .../routers/demos.py:1424 in delete_demo → db.commit()

根因：`delete_demo` 只做 `db.delete(demo)`，靠外键的 `ondelete` 级联；但
**`CollectionItem.demo_id`（models.py:659）与 `DemoView.demo_id`（models.py:675）建表时没写 ondelete**，
SQLite 又开着 `PRAGMA foreign_keys=ON` ⇒ 只要该作品被收藏过、或被浏览过，删除就撞外键约束。

修法：删之前显式清这两张表的引用行（不改表结构，存量生产库立刻生效 —— SQLite 加约束要重建表，
而 `_ensure_*` 那套迁移只会加列）。
"""

import pytest
from sqlalchemy.exc import IntegrityError

from app.database import SessionLocal
from app.models import Collection, CollectionItem, Demo, DemoView, User
from app.routers.demos import _purge_demo_references


def _mk_demo(db, slug: str, author_id: int) -> Demo:
    demo = Demo(slug=slug, title=slug, description="", status="approved", author_id=author_id)
    db.add(demo)
    db.flush()
    return demo


def _mk_user(db, username: str) -> User:
    from app.domains.identity.security import hash_password

    user = User(username=username, password_hash=hash_password("x"), role="user")
    db.add(user)
    db.flush()
    return user


@pytest.fixture()
def db(client):  # 依赖 client：临时库由它建表，否则 SessionLocal 指向空库

    s = SessionLocal()
    try:
        yield s
    finally:
        s.rollback()
        s.close()


def test_delete_demo_with_collection_and_view_rows(db):
    """有收藏 + 有浏览记录的作品：不加清理就必然撞外键（先复现，再验证修好）。"""
    user = _mk_user(db, "fk-cleanup-user")
    demo = _mk_demo(db, "fk-cleanup-demo", user.id)
    col = Collection(title="默认收藏夹", owner_id=user.id)
    db.add(col)
    db.flush()
    db.add(CollectionItem(collection_id=col.id, demo_id=demo.id))
    db.add(DemoView(demo_id=demo.id, user_id=user.id))
    db.commit()
    demo_id = demo.id

    # ① 复现：手动删（绕过清理）应当撞外键 —— 证明这个回归测试真的盯着那个失败
    with pytest.raises(IntegrityError):
        db.delete(db.get(Demo, demo_id))
        db.commit()
    db.rollback()

    # ② 走修复路径：清引用 → 再删 → 成功且引用行没了
    _purge_demo_references(db, demo_id)
    db.delete(db.get(Demo, demo_id))
    db.commit()

    assert db.get(Demo, demo_id) is None
    assert db.query(CollectionItem).filter(CollectionItem.demo_id == demo_id).count() == 0
    assert db.query(DemoView).filter(DemoView.demo_id == demo_id).count() == 0


def test_delete_endpoint_returns_204_with_references(client, admin_headers, db):
    """接口层：带收藏与浏览记录的作品，DELETE 必须是 204（不是 500）。"""
    user = _mk_user(db, "fk-cleanup-api-user")
    demo = _mk_demo(db, "fk-cleanup-api-demo", user.id)
    col = Collection(title="接口用收藏夹", owner_id=user.id)
    db.add(col)
    db.flush()
    db.add(CollectionItem(collection_id=col.id, demo_id=demo.id))
    db.add(DemoView(demo_id=demo.id, user_id=user.id))
    db.commit()

    demo_id, slug = demo.id, demo.slug  # 行删掉后不能再从对象上取属性（会触发对已删行的刷新）
    r = client.delete(f"/api/v1/demos/{slug}", headers=admin_headers)
    assert r.status_code == 204, r.text
    # 接口用自己的会话删的行；本测试的会话要先过期，否则 get() 会命中身份映射里的陈旧对象
    db.expire_all()
    assert db.get(Demo, demo_id) is None
