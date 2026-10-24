"""提示词库：公开可见性、独立投稿审核、精确正文与旧库回填。"""

from datetime import datetime, timedelta
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from app.database import SessionLocal
from app.main import app
from app.models import AuditLog, Demo, DemoModel, EntitySuggestion, Model, Prompt, Setting


@pytest.fixture()
def key(client):
    """每例独立命名与清理，避免共享数据库改变其他套件的作品池。"""
    marker = "pl-" + uuid4().hex
    yield marker
    from app import models

    with SessionLocal() as db:
        suggestion_ids = [r[0] for r in db.query(EntitySuggestion.id).filter(
            EntitySuggestion.kind == "new_prompt", EntitySuggestion.payload.contains(marker)
        ).all()]
        if suggestion_ids:
            db.query(AuditLog).filter(
                AuditLog.entity_type == "suggestion", AuditLog.entity_id.in_(suggestion_ids),
                AuditLog.after.contains('"kind": "new_prompt"'),
            ).delete(synchronize_session=False)
        db.query(EntitySuggestion).filter(
            EntitySuggestion.kind == "new_prompt", EntitySuggestion.payload.contains(marker)
        ).delete(synchronize_session=False)
        db.query(Demo).filter(Demo.slug.startswith(marker)).delete(synchronize_session=False)
        db.query(Model).filter(Model.slug.startswith(marker)).delete(synchronize_session=False)
        ids = [r[0] for r in db.query(Prompt.id).filter(Prompt.content.contains(marker)).all()]
        entry = getattr(models, "PromptLibraryEntry", None)
        if entry is not None and ids:
            db.query(entry).filter(entry.prompt_id.in_(ids)).delete(synchronize_session=False)
        if ids:
            db.query(Prompt).filter(Prompt.id.in_(ids)).delete(synchronize_session=False)
        # FTS 索引行一并清理（表不存在时静默跳过），避免孤儿 rowid 影响后续用例
        from sqlalchemy import text as _text

        from app.services.prompt_library_service import _search_index_ready

        if ids and _search_index_ready(db):
            id_list = ",".join(str(int(i)) for i in ids)  # 数据库主键整型，内联安全
            db.execute(_text(f"DELETE FROM prompt_fts WHERE rowid IN ({id_list})"))
        db.commit()


def seed_prompt(content):
    from app.services.prompt_library_service import get_or_create_prompt

    # 走服务层创建：与线上一致地同步 FTS 搜索索引（直插 Prompt 会漏索引）。
    with SessionLocal() as db:
        row = get_or_create_prompt(db, content)
        db.commit()
        return row.id


def seed_demo(key, prompt_id, *, status="approved", sites="deep", n=0, title=None):
    with SessionLocal() as db:
        content = db.get(Prompt, prompt_id).content if prompt_id else key + " legacy"
        d = Demo(
            slug=f"{key}-{n}", title=title or f"{key} example {n}",
            prompt=content, prompt_id=prompt_id, status=status, sites=sites,
            created_at=datetime(2026, 1, 1) + timedelta(minutes=n),
        )
        db.add(d)
        db.commit()
        return d.id, d.slug


def submit(client, headers, key, **extra):
    return client.post("/api/v1/prompts", headers=headers, json={
        "title": key + " 收录", "content": key + "\n做一个交互作品。", **extra,
    })


def approve(client, admin_headers, suggestion_id):
    response = client.post(
        f"/api/v1/admin/suggestions/{suggestion_id}/review",
        json={"action": "approve"}, headers=admin_headers,
    )
    assert response.status_code == 200, response.text
    return response.json()


def test_requires_login(client, key):
    with TestClient(app) as anon:
        response = submit(anon, {}, key)
    assert response.status_code == 401, response.text


def test_submission_review_and_zero_result_publication(client, auth_headers, admin_headers, key):
    headers, _ = auth_headers()
    response = submit(client, headers, key, category="interactive", source_url="https://example.com/prompt")
    assert response.status_code == 202, response.text
    result = response.json()
    assert result["status"] == "pending"
    assert client.get("/api/v1/prompts", params={"q": key}).json()["total"] == 0
    with SessionLocal() as db:
        assert db.query(Prompt).filter(Prompt.content.contains(key)).count() == 0
        s = db.get(EntitySuggestion, result["suggestion_id"])
        assert s.kind == "new_prompt" and s.confidence >= 0.6

    inbox = client.get("/api/v1/admin/suggestions", params={"kind": "new_prompt"}, headers=admin_headers)
    assert inbox.status_code == 200, inbox.text
    assert result["suggestion_id"] in {r["id"] for r in inbox.json()["items"]}
    approve(client, admin_headers, result["suggestion_id"])
    rows = client.get("/api/v1/prompts", params={"q": key}).json()
    assert rows["total"] == 1
    item = rows["items"][0]
    assert item["source"] == "contributed"
    assert item["category"] == "interactive"
    assert item["source_url"] == "https://example.com/prompt"
    assert item["demos_total"] == item["models_total"] == 0
    detail = client.get(f"/api/v1/prompts/{item['id']}").json()
    assert detail["content"] == key + "\n做一个交互作品。"
    # 列表项只带摘要；详情多出全文 content，其余字段逐一相等
    assert {k: v for k, v in detail.items() if k != "content"} == item
    results = client.get(f"/api/v1/prompts/{item['id']}/demos").json()
    assert results["total"] == 0 and results["items"] == []
    assert client.get("/api/v1/prompts", params={"q": key, "has_results": False}).json()["total"] == 1
    assert client.get("/api/v1/prompts", params={"q": key, "has_results": True}).json()["total"] == 0


def test_existing_visible_demos_group_counts_and_results_paginate(client, key):
    pid = seed_prompt(key + " shared prompt")
    a, slug_a = seed_demo(key, pid, n=0)
    b, slug_b = seed_demo(key, pid, n=1)
    hidden, _ = seed_demo(key, pid, status="pending", n=2)
    other_scope, _ = seed_demo(key, pid, sites="astra", n=3)
    with SessionLocal() as db:
        db.get(Demo, a).gen_rounds = 1
        db.get(Demo, a).gen_minutes = 3
        m = Model(slug=key + "-model", name=key + " model", status="active")
        hidden_model = Model(slug=key + "-hidden", name=key + " hidden", status="active")
        db.add_all([m, hidden_model])
        db.flush()
        db.add_all([DemoModel(demo_id=a, model_id=m.id), DemoModel(demo_id=b, model_id=m.id),
                    DemoModel(demo_id=hidden, model_id=hidden_model.id),
                    DemoModel(demo_id=other_scope, model_id=hidden_model.id)])
        db.commit()
    response = client.get("/api/v1/prompts", params={"q": key})
    assert response.status_code == 200, response.text
    item = response.json()["items"][0]
    assert item["id"] == pid and item["source"] == "demo"
    assert item["demos_total"] == 2 and item["models_total"] == 1
    assert item["title"] == key + " example 0"
    one = client.get(f"/api/v1/prompts/{pid}/demos", params={"page_size": 1}).json()
    two = client.get(f"/api/v1/prompts/{pid}/demos", params={"page_size": 1, "page": 2}).json()
    assert one["total"] == two["total"] == 2
    assert {one["items"][0]["slug"], two["items"][0]["slug"]} == {slug_a, slug_b}
    by_slug = {r["slug"]: r for r in one["items"] + two["items"]}
    assert by_slug[slug_a]["gen_rounds"] == 1 and by_slug[slug_a]["gen_minutes"] == 3
    assert by_slug[slug_b]["gen_rounds"] is None and by_slug[slug_b]["gen_minutes"] is None


def test_cached_or_pending_prompts_cannot_be_read_publicly(client, key):
    pids = [seed_prompt(key + str(i)) for i in range(3)]
    seed_demo(key, pids[1], status="pending", n=1)
    seed_demo(key, pids[2], sites="astra", n=2)
    assert client.get("/api/v1/prompts", params={"q": key}).json()["total"] == 0
    for pid in pids:
        assert client.get(f"/api/v1/prompts/{pid}").status_code == 404
        assert client.get(f"/api/v1/prompts/{pid}/demos").status_code == 404


def test_pending_idempotency_and_public_duplicate_do_not_overwrite(client, auth_headers, admin_headers, key):
    headers, _ = auth_headers()
    first = submit(client, headers, key).json()
    second = submit(client, headers, key, title="same body with another title").json()
    assert first["suggestion_id"] == second["suggestion_id"]
    approve(client, admin_headers, first["suggestion_id"])
    duplicate = submit(client, headers, key, title="overwrite title?")
    assert duplicate.status_code in (200, 202), duplicate.text
    assert duplicate.json()["status"] == "existing"
    pid = duplicate.json()["prompt_id"]
    assert client.get(f"/api/v1/prompts/{pid}").json()["title"] == key + " 收录"


def test_two_users_pending_same_body_approval_is_idempotent(client, auth_headers, admin_headers, key):
    h1, _ = auth_headers()
    h2, _ = auth_headers()
    a = submit(client, h1, key).json()["suggestion_id"]
    b = submit(client, h2, key, title=key + " 后来的标题").json()["suggestion_id"]
    assert a != b
    approve(client, admin_headers, a)
    approve(client, admin_headers, b)
    rows = client.get("/api/v1/prompts", params={"q": key}).json()
    assert rows["total"] == 1 and rows["items"][0]["title"] == key + " 收录"


def test_rejected_prompt_stays_private(client, auth_headers, admin_headers, key):
    headers, _ = auth_headers()
    sid = submit(client, headers, key).json()["suggestion_id"]
    response = client.post(f"/api/v1/admin/suggestions/{sid}/review", headers=admin_headers,
                           json={"action": "reject"})
    assert response.status_code == 200
    assert client.get("/api/v1/prompts", params={"q": key}).json()["total"] == 0


def test_exact_body_normalization_preserves_case_and_internal_whitespace(client, auth_headers, admin_headers, key):
    headers, _ = auth_headers()
    base = key + "\nKeep  Case"
    responses = [submit(client, headers, key, content=body).json() for body in
                 [" \r\n" + base.replace("\n", "\r\n") + " \n", base, base.lower(), base.replace("  ", " ")]]
    assert responses[0]["suggestion_id"] == responses[1]["suggestion_id"]
    assert len({r["suggestion_id"] for r in responses}) == 3
    for sid in {r["suggestion_id"] for r in responses}:
        approve(client, admin_headers, sid)
    rows = client.get("/api/v1/prompts", params={"q": key}).json()
    assert rows["total"] == 3
    assert base in {r["content_excerpt"] for r in rows["items"]}


@pytest.mark.parametrize("extra", [
    {"title": "   "}, {"content": " \r\n\t "}, {"title": "x" * 201},
    {"content": "x" * 20001}, {"category": "x" * 65}, {"source_url": "x" * 2049},
    {"source_url": "javascript:alert(1)"}, {"source_url": "https://"},
    {"source_url": "https://user:password@example.com/path"},
])
def test_submission_validates_fields(client, auth_headers, key, extra):
    headers, _ = auth_headers()
    assert submit(client, headers, key, **extra).status_code == 422


def test_submission_rate_limit(client, auth_headers, key):
    headers, _ = auth_headers()
    for n in range(10):
        response = submit(client, headers, key, content=key + str(n))
        assert response.status_code == 202, response.text
    assert submit(client, headers, key, content=key + "eleventh").status_code == 429


def test_filters_order_and_pagination(client, auth_headers, admin_headers, key):
    headers, _ = auth_headers()
    for i, category in enumerate(["game", "tool", "game"]):
        result = submit(client, headers, key, content=key + str(i), title=key + str(i), category=category)
        approve(client, admin_headers, result.json()["suggestion_id"])
    rows = client.get("/api/v1/prompts", params={"q": key, "category": "game"}).json()
    assert rows["total"] == 2
    items = client.get("/api/v1/prompts", params={"q": key}).json()["items"]
    pid = items[-1]["id"]
    seed_demo(key, pid)
    first = client.get("/api/v1/prompts", params={"q": key, "sort": "most_tested", "page_size": 1}).json()
    assert first["total"] == 3 and first["items"][0]["id"] == pid
    second = client.get("/api/v1/prompts", params={"q": key, "sort": "most_tested", "page_size": 1, "page": 2}).json()
    assert first["items"][0]["id"] != second["items"][0]["id"]
    assert client.get("/api/v1/prompts", params={"q": key, "page_size": 1, "page": 4}).json()["items"] == []


def test_astra_remains_read_only_and_prompts_are_not_whitelisted(client, key):
    headers = {"host": "astrademos.top"}
    for url in ["/api/v1/prompts", "/api/v1/prompts/1", "/api/v1/prompts/1/demos"]:
        assert client.get(url, headers=headers).status_code == 404
    assert submit(client, headers, key).status_code == 404


def test_backfill_missing_links_is_exact_batched_and_repeatable(client, key):
    from app.services.prompt_library_service import backfill_missing_prompt_links

    old = seed_prompt(key + "\nOriginal  Body")
    pid = seed_prompt(key + " already linked")
    ids = []
    with SessionLocal() as db:
        for i, (body, linked, status) in enumerate([
            ("  " + key + "\r\nOriginal  Body\n", None, "approved"),
            (key + "\nOriginal  Body", None, "approved"),
            (" \r\n\t ", None, "approved"),
            (key + " mismatched but linked", pid, "approved"),
            (key + " pending only", None, "pending"),
        ]):
            d = Demo(slug=f"{key}-{i}", title=key, prompt=body, prompt_id=linked, status=status)
            db.add(d)
            db.flush()
            ids.append(d.id)
        db.commit()
        before = db.get(Demo, ids[0]).updated_at
        count = backfill_missing_prompt_links(db, batch_size=2)
        db.commit()
        assert count >= 3
        db.expire_all()
        assert db.get(Demo, ids[0]).prompt_id == old
        assert db.get(Demo, ids[1]).prompt_id == old
        assert db.get(Demo, ids[2]).prompt_id is None
        assert db.get(Demo, ids[3]).prompt_id == pid
        assert db.get(Demo, ids[0]).updated_at == before
        pending_id = db.get(Demo, ids[4]).prompt_id
        assert pending_id is not None
        assert backfill_missing_prompt_links(db, batch_size=2) == 0
        db.commit()
    assert client.get(f"/api/v1/prompts/{pending_id}").status_code == 404


def test_startup_backfills_legacy_rows(client, key):
    from app.services.prompt_library_service import BACKFILL_MARK_KEY

    with SessionLocal() as db:
        d = Demo(slug=key + "-old", title=key, prompt=key + " old nonempty", status="approved")
        db.add(d)
        db.commit()
        did = d.id
        # 模拟从未跑过回填的旧库：删掉版本标记，重启才会执行回填
        db.query(Setting).filter(Setting.key == BACKFILL_MARK_KEY).delete()
        db.commit()
    with TestClient(app) as restarted:
        rows = restarted.get("/api/v1/prompts", params={"q": key}).json()
        assert rows["total"] == 1
    with SessionLocal() as db:
        assert db.get(Demo, did).prompt_id == rows["items"][0]["id"]
        # 回填完成后标记重新打上：稳态启动不再扫描
        assert db.get(Setting, BACKFILL_MARK_KEY) is not None


def test_publication_participates_in_callers_transaction(client, key):
    from app.models import PromptLibraryEntry
    from app.services.prompt_library_service import publish_prompt

    with SessionLocal() as db:
        entry = publish_prompt(db, {"title": key, "content": key + " rolled back"}, created_by=None)
        pid, eid = entry.prompt_id, entry.id
        # 模拟后续审计失败：正文、收录信息和建议审核必须一起回滚。
        db.rollback()
    with SessionLocal() as check:
        assert check.get(PromptLibraryEntry, eid) is None
        assert check.get(Prompt, pid) is None


def test_concurrent_identical_submissions_share_one_pending_record(client, auth_headers, key, monkeypatch):
    from concurrent.futures import ThreadPoolExecutor
    from threading import Barrier, BrokenBarrierError

    from app.services import suggestion_service

    headers, _ = auth_headers()
    both_in_create = Barrier(2)
    real_create = suggestion_service.create

    def delayed_create(*args, **kwargs):
        # 扩大“已查无重复、尚未创建”的竞争窗口；修复后第二请求不会进入 create。
        if kwargs.get("kind") == "new_prompt":
            try:
                both_in_create.wait(timeout=0.5)
            except BrokenBarrierError:
                pass
        return real_create(*args, **kwargs)

    monkeypatch.setattr(suggestion_service, "create", delayed_create)

    def request():
        independent = TestClient(app)
        try:
            return submit(independent, headers, key)
        finally:
            independent.close()

    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(request) for _ in range(2)]
        results = [f.result(timeout=10) for f in futures]
    assert [r.status_code for r in results] == [202, 202]
    assert len({r.json()["suggestion_id"] for r in results}) == 1
    with SessionLocal() as db:
        assert db.query(EntitySuggestion).filter(
            EntitySuggestion.kind == "new_prompt", EntitySuggestion.payload.contains(key),
            EntitySuggestion.status == "pending",
        ).count() == 1


def test_concurrent_approve_reject_has_one_decision(client, auth_headers, key):
    from concurrent.futures import ThreadPoolExecutor
    from threading import Barrier

    from fastapi import HTTPException

    from app.services import suggestion_service

    headers, _ = auth_headers()
    sid = submit(client, headers, key).json()["suggestion_id"]
    both_loaded = Barrier(2)

    def review(action):
        with SessionLocal() as db:
            s = db.get(EntitySuggestion, sid)
            assert s.status == "pending"
            both_loaded.wait(timeout=5)
            try:
                suggestion_service.review(db, s, action, actor_id=None, actor_type="system")
                return action, 200
            except HTTPException as exc:
                db.rollback()
                return action, exc.status_code

    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(review, action) for action in ("approve", "reject")]
        outcomes = [f.result(timeout=10) for f in futures]
    assert sorted(status for _, status in outcomes) == [200, 409]
    winning_action = next(action for action, status in outcomes if status == 200)
    with SessionLocal() as db:
        status = db.get(EntitySuggestion, sid).status
    assert status == ("approved" if winning_action == "approve" else "rejected")
    visible = client.get("/api/v1/prompts", params={"q": key}).json()["total"]
    assert visible == (1 if status == "approved" else 0)


def test_old_schema_can_create_library_and_backfill(tmp_path, monkeypatch):
    from sqlalchemy import create_engine, inspect
    from sqlalchemy.orm import Session

    from app import main
    from app.database import Base
    from app.services.prompt_library_service import backfill_missing_prompt_links

    legacy_engine = create_engine("sqlite:///" + (tmp_path / "legacy.db").as_posix())
    try:
        with legacy_engine.begin() as conn:
            conn.exec_driver_sql("CREATE TABLE demos (id INTEGER PRIMARY KEY, prompt TEXT NOT NULL)")
            conn.exec_driver_sql("INSERT INTO demos (id, prompt) VALUES (1, ?), (2, ?)",
                                 ("Old  Prompt\r\nKeep Case", "Old  Prompt\nKeep Case"))
        assert "prompt_id" not in {c["name"] for c in inspect(legacy_engine).get_columns("demos")}
        Base.metadata.create_all(bind=legacy_engine)
        monkeypatch.setattr(main, "engine", legacy_engine)
        # 测试库已独立置于 tmp_path；不调用绑定全局 settings 路径的生产备份函数。
        monkeypatch.setattr(main, "_sqlite_db_backup", lambda reason: None)
        main._ensure_demo_columns()
        with Session(legacy_engine) as db:
            assert backfill_missing_prompt_links(db, batch_size=1) == 2
            db.commit()
            assert backfill_missing_prompt_links(db, batch_size=1) == 0
            rows = db.query(Demo.id, Demo.prompt_id).order_by(Demo.id).all()
            assert rows[0].prompt_id == rows[1].prompt_id
            assert db.query(Prompt).count() == 1
        assert "prompt_library_entries" in inspect(legacy_engine).get_table_names()
    finally:
        legacy_engine.dispose()


def test_list_returns_excerpt_only_and_detail_returns_full(client, key):
    long_body = key + " " + "z" * 600
    pid = seed_prompt(long_body)
    short_pid = seed_prompt(key + " short body")
    seed_demo(key, pid, n=0)
    seed_demo(key, short_pid, n=1)
    rows = client.get("/api/v1/prompts", params={"q": key, "page_size": 10}).json()
    by_id = {r["id"]: r for r in rows["items"]}
    assert "content" not in by_id[pid]
    assert by_id[pid]["content_truncated"] is True
    assert len(by_id[pid]["content_excerpt"]) == 480 and by_id[pid]["content_excerpt"].endswith("z")
    assert by_id[short_pid]["content_truncated"] is False
    assert by_id[short_pid]["content_excerpt"] == key + " short body"
    full = client.get(f"/api/v1/prompts/{pid}").json()
    assert full["content"] == long_body and full["content_truncated"] is True


def test_admin_inbox_truncates_payload_and_detail_returns_full(client, auth_headers, admin_headers, key):
    headers, _ = auth_headers()
    body = key + " " + "y" * 600
    sid = submit(client, headers, key, content=body).json()["suggestion_id"]

    items = client.get("/api/v1/admin/suggestions", params={"kind": "new_prompt", "status": "all"},
                       headers=admin_headers).json()["items"]
    mine = next(i for i in items if i["id"] == sid)
    assert mine["payload"]["content_truncated"] is True
    assert len(mine["payload"]["content"]) == 480

    detail = client.get(f"/api/v1/admin/suggestions/{sid}", headers=admin_headers).json()
    assert detail["payload"]["content"] == body
    assert "content_truncated" not in detail["payload"]
    # 详情同样在 admin 门内：匿名 403
    assert client.get(f"/api/v1/admin/suggestions/{sid}").status_code == 403


def test_demo_upload_prompt_is_searchable(client, key):
    """上传/编辑路径（set_demo_prompt）创建的正文必须进 FTS 索引——稳态搜索不漏结果。

    回归点：该路径曾直插 Prompt 绕过索引同步，上传的作品在 ≥3 字符搜索下不可见。
    """
    from app.services.model_service import set_demo_prompt

    body = key + " 量子纠缠独有正文串"
    with SessionLocal() as db:
        d = Demo(slug=key + "-upload", title=key, prompt=body, status="approved", sites="deep")
        db.add(d)
        db.flush()
        set_demo_prompt(db, d)
        db.commit()

    rows = client.get("/api/v1/prompts", params={"q": "量子纠缠独有正文串"}).json()
    assert rows["total"] == 1
    assert rows["items"][0]["title"] == key
    assert rows["items"][0]["content_excerpt"].startswith(key)


def test_search_falls_back_when_fts_unready_or_emptied(client, key):
    """就绪标记缺失（并发首启窗口）与索引被清空（人为损坏）都必须回退 LIKE，不静默漏正文。

    回归点：表在数据空时 `_fts_candidate_ids` 返回 [] 曾导致正文条件被整体省略、只搜标题。
    """
    from sqlalchemy import text as sql_text

    from app.services import prompt_library_service as svc

    body = key + " 就绪标记回退正文串"
    seed_demo(key, seed_prompt(body))

    def hit():
        rows = client.get("/api/v1/prompts", params={"q": "就绪标记回退正文串"}).json()
        assert rows["total"] == 1, rows
        return rows["items"][0]["id"]

    with SessionLocal() as db:
        pid = hit()
        # a) 标记缺失（并发首启窗口：表可见但首灌未提交）→ 必须回退 LIKE
        db.query(Setting).filter(Setting.key == svc.FTS_READY_KEY).delete()
        db.commit()
        assert svc._fts_candidate_ids(db, "就绪标记回退正文串") is None
        assert hit() == pid
        # b) 标记在场但索引被清空（人为损坏）→ 空候选回退 LIKE，不静默漏
        svc.mark_search_index_ready()
        db.execute(sql_text("DELETE FROM prompt_fts"))
        db.commit()
        assert svc._fts_candidate_ids(db, "就绪标记回退正文串") is None
        assert hit() == pid
        # 恢复现场：重灌全部缺位正文 + 重新打标，避免污染其他用例
        svc.sync_prompt_search_index(db)
        db.commit()
    svc.mark_search_index_ready()
    with SessionLocal() as db:
        assert svc._fts_candidate_ids(db, "就绪标记回退正文串") is not None
        assert hit() == pid


def test_backfill_mark_skips_steady_state(client, key):
    from app.services.prompt_library_service import (
        BACKFILL_MARK_KEY,
        backfill_missing_prompt_links,
        backfill_missing_prompt_links_once,
    )

    with SessionLocal() as db:
        d = Demo(slug=key + "-mark", title=key, prompt=key + " after mark", status="approved")
        db.add(d)
        db.commit()
        did = d.id
        # 首轮启动已打标：once 路径跳过（稳态零扫描），直跑函数仍可用于手动修复
        assert db.get(Setting, BACKFILL_MARK_KEY) is not None
        assert backfill_missing_prompt_links_once(db) == 0
        db.commit()
        assert db.get(Demo, did).prompt_id is None
        assert backfill_missing_prompt_links(db) >= 1
        db.commit()
        assert db.get(Demo, did).prompt_id is not None


def test_search_fts_matches_like_semantics(client, key):
    from sqlalchemy import text as sql_text

    from app.services import prompt_library_service as svc

    bodies = [
        key + " Alpha Case",
        (key + " alpha case").upper(),
        key + " 番茄时钟计划",
        key + " 番茄炒蛋",
        key + ' quote"inside',
        key + " MiniTest",
    ]
    alpha_pids = set()
    for n, body in enumerate(bodies):
        pid = seed_prompt(body)
        if "alpha" in body.lower():
            alpha_pids.add(pid)
        seed_demo(key, pid, n=n)

    def result_ids(q):
        rows = client.get("/api/v1/prompts", params={"q": q, "page_size": 100}).json()["items"]
        return sorted(r["id"] for r in rows)

    def like_only_ids(q):
        with SessionLocal() as db:
            original = svc._fts_candidate_ids
            svc._fts_candidate_ids = lambda session, value: None
            try:
                items, _ = svc.list_prompts(db, scope="deep", q=q, page_size=100)
            finally:
                svc._fts_candidate_ids = original
            return sorted(r["id"] for r in items)

    with SessionLocal() as db:
        assert svc._search_index_ready(db) is True
        # 常见中文二字词低于 trigram 下限：固定走 LIKE 回退
        assert svc._fts_candidate_ids(db, "番茄") is None
        plan = " ".join(str(row) for row in db.execute(
            sql_text("EXPLAIN QUERY PLAN SELECT rowid FROM prompt_fts WHERE prompt_fts MATCH :m"),
            {"m": '"alpha"'}).all())
        assert "prompt_fts" in plan

    for q in ["alpha", "ALPHA", "Case", "番茄时钟", "番茄炒蛋", 'quote"inside', key + " MiniTest", "没有匹配词组"]:
        assert result_ids(q) == like_only_ids(q), q
    assert alpha_pids <= set(result_ids("alpha"))  # 大小写两条变体都命中，FTS 与 LIKE 口径一致
