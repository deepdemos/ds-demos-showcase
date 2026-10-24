"""题目实体公开接口（v2 B1）：列表 / 详情（含按模型对比=Benchmark 数据）/ 规则建议 / 独立出题。"""

import json

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy.orm import Session

from ..database import get_db
from ..domains.identity.deps import current_user, optional_user
from ..models import Demo, DemoTask, EntitySuggestion, Task, User
from ..schemas import TaskDetailOut, TaskListOut, TaskProposalIn, TaskProposalOut, TaskSuggestItemOut
from ..serializers import preload_demo_relations, serialize_demo
from ..services import ratelimit, suggestion_service, task_service

router = APIRouter(prefix="/tasks", tags=["tasks"])

# 独立出题限流：每用户每小时 10 次（全站限流单一实现，KB-21）
TASK_PROPOSE_RATE_LIMIT = 10
# 与上传页 propose_task 同一置信档（demos.USER_MATCH_CONFIDENCE=0.98）：必进收件箱人工审，
# 够不到 AUTO_ACCEPT(0.99) —— 审核语义与上传提议新题完全一致
PROPOSE_CONFIDENCE = 0.98


@router.get("/suggest", response_model=list[TaskSuggestItemOut])
def suggest_tasks(
    q: str = Query(min_length=2, max_length=2000, description="标题/提示词/描述拼接文本"),
    limit: int = Query(default=5, ge=1, le=10),
    db: Session = Depends(get_db),
):
    """规则层（TF-IDF）相似任务建议：纯读、无 LLM；供上传页「挂到已有题目」参考。"""
    return [TaskSuggestItemOut(**s) for s in task_service.suggest_for_demo(db, q, "", "")[:limit]]


@router.get("", response_model=TaskListOut)
def list_tasks(
    status: str | None = Query(default=None, description="缺省 active；candidate 仅管理员"),
    q: str | None = None,
    category: str | None = None,
    sort: str = Query(default="demos", pattern="^(demos|newest)$"),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    # KB-15：公开列表只出 active。旧实现把 status 原样透传，`?status=hidden|merged|candidate`
    # 都能读到未确认/已下架题目（实测 200）。
    if status and status not in ("active", "candidate"):
        raise HTTPException(status_code=422, detail="status 需为 active/candidate", )
    if status == "candidate" and not (user is not None and user.role == "admin"):
        raise HTTPException(status_code=403, detail="仅管理员可查看待确认题目", )
    items, total = task_service.list_tasks(
        db, status=status, q=q, category=category, sort=sort, page=page, page_size=page_size
    )
    return TaskListOut(items=items, total=total, page=page, page_size=page_size)


@router.post("", status_code=202, response_model=TaskProposalOut)
def propose_task(
    payload: TaskProposalIn,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    """独立出题（登录用户）：题目不再只能是作品的附属品。

    与上传页「提议新题」走同一条审核路径：只落 pending EntitySuggestion(kind=new_task)，
    管理员在收件箱批准后才由 suggestion_service.review 调 create_task(status=active) 公开。
    不开放匿名（current_user 必须登录），不直建 Task —— 权限面与审核语义零新增。
    """
    title = payload.title.strip()
    if not title:
        raise HTTPException(status_code=422, detail="title 必填（不能全是空白）")
    description = payload.description.strip()
    category = (payload.category or "").strip() or None

    ratelimit.hit(f"task:propose:{user.id}", TASK_PROPOSE_RATE_LIMIT, 3600)

    # 与既有 active 题目逐字同名 → 409 指路（按标题「猜」同题不做，但完全同名值得挡，
    # 否则收件箱里会堆出只差一个字的重复题让管理员人工分辨）
    exists = db.query(Task).filter(Task.title == title, Task.status == "active").first()
    if exists is not None:
        raise HTTPException(status_code=409, detail=f"已存在同名题目：/tasks/{exists.slug}")

    # 同一用户已提交过同题名提案 → 幂等返回不堆箱（dedupe=False 后，业务键去重在这里做）
    for s in (
        db.query(EntitySuggestion)
        .filter(
            EntitySuggestion.kind == "new_task",
            EntitySuggestion.status == "pending",
            EntitySuggestion.created_by == user.id,
        )
        .all()
    ):
        try:
            data = json.loads(s.payload or "{}")
        except json.JSONDecodeError:
            continue
        if isinstance(data, dict) and (data.get("title") or "").strip() == title:
            return TaskProposalOut(status="pending", suggestion_id=s.id, duplicate=True)

    s = suggestion_service.create(
        db,
        kind="new_task",
        payload={
            "title": title,
            "description": description,
            **({"category": category} if category else {}),
        },
        confidence=PROPOSE_CONFIDENCE,
        source="user",
        created_by=user.id,
        dedupe=False,  # 三条定位键全 None 会被判成同一条；去重已由上面的同用户同题名检查承担
    )
    if s is None:  # 防御：dedupe=False 时 create 不会返回 None
        raise HTTPException(status_code=500, detail="提案入箱失败，请重试")
    db.commit()  # KB-16：create 只 flush，事务边界归调用方
    return TaskProposalOut(status="pending", suggestion_id=s.id, duplicate=False)


@router.get("/{slug}", response_model=TaskDetailOut)
def task_detail(
    slug: str,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    # KB-15：merged/hidden 直接 404；candidate 仅管理员可见
    is_admin = bool(user is not None and user.role == "admin")
    detail = task_service.task_detail(db, slug, allow_candidate=is_admin)
    if detail is None:
        raise HTTPException(status_code=404, detail="题目不存在或未上架", )
    task = task_service.get_by_slug(db, slug)
    demos = (
        db.query(Demo)
        .join(DemoTask, DemoTask.demo_id == Demo.id)
        .filter(DemoTask.task_id == task.id, Demo.status == "approved")
        .order_by(Demo.rating_avg.desc(), Demo.created_at.desc(), Demo.id.desc())
        .limit(24)
        .all()
    )
    preload_demo_relations(db, demos)
    detail["demos"] = [serialize_demo(db, d) for d in demos]
    # 链条视图：题面 + 逐作品的证据行（一致性在服务端算，见 task_service.task_chain）
    detail["chain"] = task_service.task_chain(db, task)
    return TaskDetailOut(**detail)
