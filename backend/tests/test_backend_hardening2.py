"""后端收尾轮 P1/P2 回归（KB-16..KB-21）。

事务边界、治理留痕、计数一致性、N+1、限流收口 —— 每条都注明「修之前会怎样」。
"""

import json

import pytest
from fastapi import HTTPException

from app.models import AuditLog, Demo, DemoRating, DemoTask, ForumReply, ForumTopic, Model, Task, User


def _db():
    from app.database import SessionLocal

    return SessionLocal()


def _upload(client, headers, title, tags=("model:dsv4-flash", "type:game")):
    files = {"file": ("index.html", f"<!doctype html><body>{title}</body>".encode(), "text/html")}
    return client.post(
        "/api/v1/demos",
        headers=headers,
        data={"title": title, "description": "hardening", "demo_type": "web",
              "tags": json.dumps(list(tags)), "model_hint": "回归"},
        files=files,
    )


def _count_sql(fn):
    """执行 fn() 期间发生的 SQL 条数。"""
    from sqlalchemy import event

    from app.database import engine

    counter = {"n": 0}

    def _listener(conn, cursor, statement, params, context, executemany):
        counter["n"] += 1

    event.listen(engine, "before_cursor_execute", _listener)
    try:
        fn()
    finally:
        event.remove(engine, "before_cursor_execute", _listener)
    return counter["n"]


# ---------------- KB-16：建议审核的事务边界 ----------------


def test_suggestion_create_does_not_commit_early(client):
    """create() 只 flush：调用方回滚后行不该存在（修前内部 commit，回滚无效）。"""
    from app.services import suggestion_service

    db = _db()
    try:
        s = suggestion_service.create(
            db, kind="new_model", payload={"name": "KB16 Rollback"}, confidence=0.9, source="inferred"
        )
        assert s is not None
        sid = s.id
        db.rollback()
        assert db.get(type(s), sid) is None, "create() 自行提交了，事务边界失效"
    finally:
        db.close()


def test_suggestion_approve_rolls_back_entity_when_execution_fails(client, admin_headers, monkeypatch):
    """批准过程中失败 → 实体与建议状态一起回滚（修前实体已 commit，留下「已建实体 + 仍 pending」）。"""
    from app.services import suggestion_service, task_service

    db = _db()
    try:
        s = suggestion_service.create(
            db, kind="new_task", payload={"title": "KB16 原子题", "demo_ids": []},
            confidence=0.9, source="inferred", ref_id=987654,  # 独立去重键，别和别的用例串
        )
        sid = s.id
        db.commit()
    finally:
        db.close()

    def _boom(*a, **kw):
        raise RuntimeError("模拟执行失败")

    monkeypatch.setattr(task_service, "attach_demos", _boom)
    # demo_ids 为空时不会走到 attach；这里直接让 create_task 之后失败
    monkeypatch.setattr(task_service, "create_task", _boom)
    with pytest.raises(Exception):
        db = _db()
        try:
            s = db.get(type(s) if False else __import__("app.models", fromlist=["EntitySuggestion"]).EntitySuggestion, sid)
            suggestion_service.review(db, s, "approve", actor_id=1)
        finally:
            db.close()

    db = _db()
    try:
        from app.models import EntitySuggestion

        assert db.query(Task).filter(Task.title == "KB16 原子题").first() is None, "实体没跟着回滚"
        row = db.get(EntitySuggestion, sid)
        assert row is not None and row.status == "pending", "建议状态被提前改掉了"
    finally:
        db.close()


# ---------------- KB-17：治理动作留痕 ----------------


def test_forum_ban_and_topic_delete_are_audited(client, admin_headers, auth_headers):
    """封禁用户 / 删主题都要留痕（修前 forum.py 完全没有 audit_service）。"""
    h, name = auth_headers()
    db = _db()
    try:
        uid = db.query(User.id).filter(User.username == name).scalar()
    finally:
        db.close()
    assert client.post(f"/api/v1/forum/admin/users/{uid}/ban", headers=admin_headers).status_code == 204
    # 封禁后该用户登录会被拒（403），这里只验证审计
    db = _db()
    try:
        row = (
            db.query(AuditLog)
            .filter(AuditLog.entity_type == "user", AuditLog.entity_id == uid)
            .order_by(AuditLog.id.desc())
            .first()
        )
    finally:
        db.close()
    assert row is not None and row.after and "banned" in (row.after or "")

    # 主题删除留痕
    db = _db()
    try:
        t = ForumTopic(title="KB17 待删主题", content="x", author_id=uid, status="normal")
        db.add(t)
        db.commit()
        tid = t.id
    finally:
        db.close()
    assert client.delete(f"/api/v1/forum/admin/topics/{tid}", headers=admin_headers).status_code == 204
    db = _db()
    try:
        row = (
            db.query(AuditLog)
            .filter(AuditLog.entity_type == "forum_topic", AuditLog.entity_id == tid)
            .first()
        )
    finally:
        db.close()
    assert row is not None and row.before


def test_audit_filter_accepts_new_entity_types(client, admin_headers):
    """审计页可按 forum_topic/user 等新类型筛选（修前白名单只有 model/task/tag/suggestion/demo）。"""
    r = client.get("/api/v1/admin/audit?entity_type=user", headers=admin_headers)
    assert r.status_code == 200, r.text
    assert "user" in r.json()["entity_types"] and "forum_topic" in r.json()["entity_types"]
    assert client.get("/api/v1/admin/audit?entity_type=bogus", headers=admin_headers).status_code == 422


# ---------------- KB-18：计数与冗余列 ----------------


def test_delete_parent_reply_adjusts_count_by_subtree(client, admin_headers, auth_headers):
    """删父回复按整棵子树的 normal 回复数扣减（修前只 -1，级联删掉的孩子不计数）。"""
    h, name = auth_headers()
    up = _upload(client, h, "KB18 主题作品")
    db = _db()
    try:
        u = db.query(User).filter(User.username == name).first()
        u.trust_level = 1  # 免审核，回复才是 normal（否则父回复处于 reviewing，子回复会被 422）
        u.need_review = False
        t = ForumTopic(title="KB18 主题", content="x", author_id=u.id, status="normal")
        db.add(t)
        db.commit()
        tid = t.id
    finally:
        db.close()
    r = client.post(f"/api/v1/forum/topics/{tid}/replies", headers=h, json={"content": "父回复"})
    assert r.status_code == 201, r.text
    parent_id = r.json()["id"]
    r2 = client.post(
        f"/api/v1/forum/topics/{tid}/replies", headers=h, json={"content": "子回复", "parent_id": parent_id}
    )
    assert r2.status_code == 201, r2.text
    db = _db()
    try:
        assert db.get(ForumTopic, tid).reply_count == 2
    finally:
        db.close()

    assert client.delete(f"/api/v1/forum/admin/replies/{parent_id}", headers=admin_headers).status_code == 204
    db = _db()
    try:
        assert db.get(ForumTopic, tid).reply_count == 0, "级联删除后计数没有按子树扣减"
    finally:
        db.close()


def test_rating_redundant_columns_match_rows(client, admin_headers):
    """冗余评分列必须与 demo_ratings 行数/总和一致（原子更新）。"""
    up = _upload(client, admin_headers, "KB18 评分作品")
    slug = up.json()["slug"]
    # 清掉登录 Cookie 才是真匿名评分（否则两个 device_id 会归到同一个 user rater_key）
    client.cookies.clear()
    for device in ("device-aaaa", "device-bbbb"):
        r = client.post(
            f"/api/v1/demos/{slug}/rating", json={"score": 5, "device_id": device}
        )
        assert r.status_code == 200, r.text
    db = _db()
    try:
        demo = db.query(Demo).filter(Demo.slug == slug).first()
        rows = db.query(DemoRating).filter(DemoRating.demo_id == demo.id).all()
        assert demo.rating_count == len(rows) == 2
        assert demo.rating_sum == sum(r.score for r in rows)
        assert demo.rating_god == 2
        assert demo.rating_avg == 5.0
    finally:
        db.close()


# ---------------- KB-19：N+1 ----------------


def test_pending_attribution_query_count_is_bounded(client, admin_headers):
    """归属工作台的 SQL 条数与作品数解耦（修前每件作品一次全表扫 Model，8 件 19 条）。"""
    from app.services import model_service

    for i in range(8):
        assert _upload(client, admin_headers, f"KB19 兜底作品{i}", ["model:unspecified", "type:game"]).status_code == 201
    db = _db()
    try:
        n = _count_sql(lambda: model_service.pending_attribution(db, limit_models=5, limit_demos=60))
    finally:
        db.close()
    assert n <= 15, f"归属清单 SQL 条数 {n}（应与作品数解耦）"


def test_attach_demos_query_count_is_bounded(client, admin_headers):
    """批量挂题的 SQL 条数不随 id 数线性增长（修前每 id 两条，实测 9 件 21 条）。"""
    from app.services import task_service

    ids = []
    for i in range(8):
        up = _upload(client, admin_headers, f"KB19 挂题作品{i}")
        ids.append(up.json()["id"])
    t = client.post("/api/v1/admin/tasks", headers=admin_headers, json={"title": "KB19 批量题", "status": "active"}).json()
    db = _db()
    try:
        tk = task_service.get_task_or_404(db, t["slug"])
        n = _count_sql(lambda: task_service.attach_demos(db, tk, ids))
    finally:
        db.close()
    assert n <= 8, f"批量挂题 SQL 条数 {n}"


def test_admin_users_query_count_is_bounded(client, admin_headers):
    """管理端用户列表用聚合取作品数（修前逐用户懒加载 demos）。"""
    n = _count_sql(lambda: client.get("/api/v1/admin/users", headers=admin_headers))
    assert n <= 6, f"/admin/users SQL 条数 {n}"


def test_forum_topic_list_query_count_is_bounded(client, admin_headers):
    """论坛主题列表的互动汇总批量取（修前逐主题 1~2 条）。"""
    db = _db()
    try:
        for i in range(10):
            db.add(ForumTopic(title=f"KB19 主题{i}", content="x", author_id=1, status="normal"))
        db.commit()
    finally:
        db.close()
    n = _count_sql(lambda: client.get("/api/v1/forum/topics?page_size=10"))
    assert n <= 8, f"论坛列表 SQL 条数 {n}"


# ---------------- KB-21：限流收口 ----------------


def test_ratelimit_bounded_keyspace_and_retry_after(client):
    """限流键空间有界 + 429 带 Retry-After（修前每 IP 一个永不回收的键）。"""
    from app.services import ratelimit

    ratelimit.reset()
    for i in range(ratelimit.MAX_KEYS + 500):
        ratelimit.hit(f"k{i}", 5, 60)
    assert ratelimit.size() <= ratelimit.MAX_KEYS, "键空间没有上界"

    ratelimit.reset()
    for _ in range(3):
        ratelimit.hit("same", 3, 60)
    with pytest.raises(HTTPException) as e:
        ratelimit.hit("same", 3, 60)
    assert e.value.status_code == 429
    assert "Retry-After" in (e.value.headers or {})
    ratelimit.reset()


def test_login_is_rate_limited(client):
    """登录失败限流（修前可无限次爆破口令）。"""
    from app.domains.identity.auth import LOGIN_RATE
    from app.services import ratelimit

    ratelimit.reset()
    last = None
    for _ in range(LOGIN_RATE + 2):
        last = client.post("/api/v1/auth/login", json={"username": "nobody-kb21", "password": "wrong-password"})
    assert last is not None and last.status_code == 429, last.status_code if last else None
    ratelimit.reset()


# ---------------- KB-24：链接校验不阻塞请求线程 ----------------


def test_link_dns_check_is_cached_and_time_bounded(monkeypatch):
    """慢解析域名最多阻塞 1s 且结果进缓存（修前请求线程里无超时 getaddrinfo）。"""
    import time as _time

    from app.services import forum_service

    calls = {"n": 0}

    def _slow(*a, **kw):
        calls["n"] += 1
        _time.sleep(5)
        return []

    monkeypatch.setattr(forum_service.socket, "getaddrinfo", _slow)
    forum_service._dns_cache.clear()

    t0 = _time.time()
    assert forum_service._host_verdict("kb24-slow.example") is None  # 超时 → fail-closed
    elapsed = _time.time() - t0
    assert elapsed < 2.0, f"解析阻塞了 {elapsed:.1f}s（应 ≤1s 超时）"

    # 第二次同域名走缓存，不再调用解析
    assert forum_service._host_verdict("kb24-slow.example") is None
    assert calls["n"] == 1, "解析结果没有进缓存"
    forum_service._dns_cache.clear()


def test_link_validation_rejects_private_literal_and_allows_public_text(monkeypatch):
    """字面量内网地址直接拒；公网域名放行（判定不变）。"""
    from fastapi import HTTPException

    from app.services import forum_service

    with pytest.raises(HTTPException):
        forum_service.validate_links("看 http://127.0.0.1:8000/admin")
    monkeypatch.setattr(forum_service, "_host_verdict", lambda host: False)
    forum_service.validate_links("参考 https://example.org/paper")


# ---------------- KB-25：举报与标签过滤 ----------------


def test_report_validates_target_and_dedupes(client, admin_headers, auth_headers):
    """举报目标必须存在；同一人同目标重复举报 409（修前都能堆）。"""
    h, name = auth_headers()
    db = _db()
    try:
        u = db.query(User).filter(User.username == name).first()
        t = ForumTopic(title="KB25 被举报主题", content="x", author_id=u.id, status="normal")
        db.add(t)
        db.commit()
        tid = t.id
    finally:
        db.close()

    r = client.post(
        "/api/v1/forum/reports", headers=h, json={"target_type": "topic", "target_id": 999999, "reason": "x"}
    )
    assert r.status_code == 404, r.text

    r = client.post(
        "/api/v1/forum/reports", headers=h, json={"target_type": "topic", "target_id": tid, "reason": "违规"}
    )
    assert r.status_code == 201, r.text
    r2 = client.post(
        "/api/v1/forum/reports", headers=h, json={"target_type": "topic", "target_id": tid, "reason": "再举报"}
    )
    assert r2.status_code == 409, r2.text


def test_forum_tag_filter_is_exact_match(client, admin_headers):
    """标签过滤精确匹配（修前 ilike 子串："demo" 会命中 "demoscene"）。"""
    db = _db()
    try:
        db.add(ForumTopic(title="KB25 标签A", content="x", author_id=1, tags="demo,特效", status="normal"))
        db.add(ForumTopic(title="KB25 标签B", content="x", author_id=1, tags="demoscene", status="normal"))
        db.commit()
    finally:
        db.close()
    r = client.get("/api/v1/forum/topics?tag=demo&page_size=50")
    assert r.status_code == 200, r.text
    titles = [i["title"] for i in r.json()["items"]]
    assert "KB25 标签A" in titles and "KB25 标签B" not in titles, titles


# ---------------- KB-26：同步单跑与别名确定性 ----------------


def test_start_sync_is_single_flight(monkeypatch):
    """running 检查与置位同锁：并发两次只有一次启动（修前 TOCTOU 可双跑）。"""
    import threading as _threading
    import time as _time

    from app.services import oss_sync

    def _fake_job(force):
        _time.sleep(0.3)
        oss_sync._update_job(running=False, finished_at=_time.time())

    monkeypatch.setattr(oss_sync, "run_sync_job", _fake_job)
    oss_sync._update_job(running=False)
    assert oss_sync.start_sync() is True
    assert oss_sync.start_sync() is False, "同一时刻启动了两个同步任务"
    for _ in range(40):
        if not oss_sync.get_sync_status().get("running"):
            break
        _time.sleep(0.05)


def test_alias_map_resolution_is_deterministic(client):
    """同规范化键的两个实体归属稳定（修前 setdefault 取决于查询顺序）。"""
    from app.services import matching_service

    db = _db()
    try:
        first = Model(slug="kb26-foo-bar-a", name="KB26 FooBar", status="active", resolution="exact")
        db.add(first)
        db.flush()
        second = Model(slug="kb26-foo-bar-b", name="KB26 Foo-Bar", status="active", resolution="exact")
        db.add(second)
        db.commit()
        first_id = first.id
        matching_service.invalidate_alias_cache()
        hits = {matching_service.match_model(db, "kb26 foobar").id for _ in range(3)}
    finally:
        db.close()
    assert hits == {first_id}, f"归属不稳定：{hits}"
