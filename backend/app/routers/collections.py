"""收藏夹：作品收藏 + 可命名可分享的公开收藏夹。

设计要点（与前端契约一致，见 docs 与 frontend/src/api/types.ts）：
  · 每个用户有一个 **is_default 的「我的收藏」**（首次用到时自动创建，不可删/不可改名/不可转公开）；
    一键收藏进它 → "收藏"这个动作永远只点一下。
  · **私密夹对他人/匿名一律 404**（不是 403）—— 403 会泄露"存在但你没权限"。
  · 同一夹内重复加入 = 幂等（不报错、不产生第二行）。
  · 上限：每人 20 个夹、每夹 500 件（超出 400 并给明确文案）。
"""

from __future__ import annotations

from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..database import get_db
from ..domains.identity.deps import current_user, optional_user
from ..models import Collection, CollectionItem, Demo, User, utcnow
from ..schemas import (
    CollectionCreateIn,
    CollectionItemPage,
    CollectionOut,
    CollectionUpdateIn,
    FavoriteStatus,
    FavoriteToggleIn,
    CollectionItemIn,
)
from ..serializers import serialize_demo

router = APIRouter(tags=["collections"])

MAX_COLLECTIONS = 20
MAX_ITEMS = 500
DEFAULT_TITLE = "我的收藏"


# ---------- 内部助手 ----------

def _get_owned(db: Session, collection_id: int, user: User) -> Collection:
    c = db.get(Collection, collection_id)
    if c is None or c.owner_id != user.id:
        raise HTTPException(status_code=404, detail="收藏夹不存在")
    return c


def default_collection(db: Session, user: User) -> Collection:
    """取（必要时创建）默认夹。放在这里而不是注册时创建 —— 老用户也有，且不写无用行。"""
    c = (
        db.query(Collection)
        .filter(Collection.owner_id == user.id, Collection.is_default.is_(True))
        .first()
    )
    if c is None:
        c = Collection(owner_id=user.id, title=DEFAULT_TITLE, is_default=True, visibility="private")
        db.add(c)
        db.commit()
        db.refresh(c)
    return c


def _cover_urls(db: Session, collection_id: int, limit: int = 3) -> list[str]:
    rows = (
        db.query(Demo.cover_url)
        .join(CollectionItem, CollectionItem.demo_id == Demo.id)
        .filter(CollectionItem.collection_id == collection_id, Demo.cover_url != "")
        .order_by(CollectionItem.created_at.desc())
        .limit(limit)
        .all()
    )
    return [r[0] for r in rows]


def _to_out(db: Session, c: Collection, owner_username: str) -> CollectionOut:
    count = db.query(func.count(CollectionItem.id)).filter(CollectionItem.collection_id == c.id).scalar() or 0
    return CollectionOut(
        id=c.id,
        owner_username=owner_username,
        title=c.title,
        description=c.description or "",
        visibility=c.visibility,
        is_default=bool(c.is_default),
        item_count=int(count),
        cover_urls=_cover_urls(db, c.id),
        updated_at=c.updated_at,
    )


# ---------- 我的收藏夹 ----------

@router.get("/me/collections")
def list_my_collections(db: Session = Depends(get_db), user: User = Depends(current_user)):
    default_collection(db, user)  # 保证默认夹存在（UI 一进来就能看到它）
    rows = (
        db.query(Collection)
        .filter(Collection.owner_id == user.id)
        .order_by(Collection.is_default.desc(), Collection.updated_at.desc())
        .all()
    )
    return {"items": [_to_out(db, c, user.username) for c in rows]}


@router.post("/me/collections", response_model=CollectionOut, status_code=201)
def create_collection(body: CollectionCreateIn, db: Session = Depends(get_db), user: User = Depends(current_user)):
    title = (body.title or "").strip()
    if not title:
        raise HTTPException(status_code=400, detail="名称不能为空")
    # 先确保默认夹存在：否则"上限 20"会被隐式默认夹绕过（实测：19 个自建 + 隐式默认 = 已满却仍能建）
    default_collection(db, user)
    total = db.query(func.count(Collection.id)).filter(Collection.owner_id == user.id).scalar() or 0
    if total >= MAX_COLLECTIONS:
        raise HTTPException(status_code=400, detail=f"最多 {MAX_COLLECTIONS} 个收藏夹")
    dup = (
        db.query(Collection)
        .filter(Collection.owner_id == user.id, Collection.title == title)
        .first()
    )
    if dup:
        raise HTTPException(status_code=400, detail="已有同名收藏夹")
    c = Collection(
        owner_id=user.id,
        title=title,
        description=(body.description or "").strip()[:200],
        visibility="public" if body.visibility == "public" else "private",
        is_default=False,
    )
    db.add(c)
    db.commit()
    db.refresh(c)
    return _to_out(db, c, user.username)


@router.patch("/me/collections/{collection_id}", response_model=CollectionOut)
def update_collection(
    collection_id: int,
    body: CollectionUpdateIn,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    c = _get_owned(db, collection_id, user)
    if body.title is not None:
        if c.is_default:
            raise HTTPException(status_code=400, detail="默认收藏夹不可改名")
        title = body.title.strip()
        if not title:
            raise HTTPException(status_code=400, detail="名称不能为空")
        c.title = title
    if body.description is not None:
        c.description = body.description.strip()[:200]
    if body.visibility is not None:
        if c.is_default:
            raise HTTPException(status_code=400, detail="默认收藏夹不可转公开")
        c.visibility = "public" if body.visibility == "public" else "private"
    c.updated_at = utcnow()
    db.commit()
    db.refresh(c)
    return _to_out(db, c, user.username)


@router.delete("/me/collections/{collection_id}", status_code=204)
def delete_collection(collection_id: int, db: Session = Depends(get_db), user: User = Depends(current_user)):
    c = _get_owned(db, collection_id, user)
    if c.is_default:
        raise HTTPException(status_code=400, detail="默认收藏夹不可删除")
    db.query(CollectionItem).filter(CollectionItem.collection_id == c.id).delete()
    db.delete(c)
    db.commit()
    return None


@router.get("/me/collections/{collection_id}/items")
def list_collection_items(
    collection_id: int,
    page: int = Query(1, ge=1),
    page_size: int = Query(20, ge=1, le=100),
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    c = _get_owned(db, collection_id, user)
    return _page_items(db, c, page, page_size, user.id)


@router.post("/me/collections/{collection_id}/items", status_code=204)
def add_item(
    collection_id: int,
    body: CollectionItemIn,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    c = _get_owned(db, collection_id, user)
    _add(db, c, body.slug, user)
    return None


@router.delete("/me/collections/{collection_id}/items/{slug}", status_code=204)
def remove_item(
    collection_id: int,
    slug: str,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    c = _get_owned(db, collection_id, user)
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is not None:
        db.query(CollectionItem).filter(
            CollectionItem.collection_id == c.id, CollectionItem.demo_id == demo.id
        ).delete()
        c.updated_at = utcnow()
        db.commit()
    return None


# ---------- 收藏状态 / 一键收藏 ----------

@router.get("/me/favorites/status", response_model=FavoriteStatus)
def favorite_status(
    slug: str = Query(...),
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        return FavoriteStatus(favorited=False, collection_ids=[])
    ids = [
        row[0]
        for row in db.query(CollectionItem.collection_id)
        .join(Collection, Collection.id == CollectionItem.collection_id)
        .filter(Collection.owner_id == user.id, CollectionItem.demo_id == demo.id)
        .all()
    ]
    return FavoriteStatus(favorited=bool(ids), collection_ids=ids)


@router.post("/me/favorites/toggle", response_model=FavoriteStatus)
def toggle_favorite(
    body: FavoriteToggleIn,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    """一键收藏/取消：不指定夹时走默认夹（"收藏"永远只点一下）。"""
    if body.collection_id:
        c = _get_owned(db, body.collection_id, user)
    else:
        c = default_collection(db, user)
    demo = db.query(Demo).filter(Demo.slug == body.slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="作品不存在")
    exists = (
        db.query(CollectionItem)
        .filter(CollectionItem.collection_id == c.id, CollectionItem.demo_id == demo.id)
        .first()
    )
    if exists:
        db.delete(exists)
        c.updated_at = utcnow()
        db.commit()
    else:
        _add(db, c, body.slug, user)
    return favorite_status(slug=body.slug, db=db, user=user)


# ---------- 公开侧 ----------

@router.get("/users/{username}/collections")
def list_public_collections(username: str, db: Session = Depends(get_db)):
    owner = db.query(User).filter(User.username == username).first()
    if owner is None:
        raise HTTPException(status_code=404, detail="用户不存在")
    rows = (
        db.query(Collection)
        .filter(Collection.owner_id == owner.id, Collection.visibility == "public")
        .order_by(Collection.updated_at.desc())
        .all()
    )
    return {"items": [_to_out(db, c, owner.username) for c in rows]}


@router.get("/collections/{collection_id}", response_model=CollectionOut)
def get_collection(
    collection_id: int,
    db: Session = Depends(get_db),
    viewer: User | None = Depends(optional_user),
):
    c = db.get(Collection, collection_id)
    is_owner = c is not None and viewer is not None and c.owner_id == viewer.id
    # 不存在 与 无权 一律 404：不泄露私密夹的存在性
    if c is None or (c.visibility != "public" and not is_owner):
        raise HTTPException(status_code=404, detail="收藏夹不存在")
    owner = db.get(User, c.owner_id)
    return _to_out(db, c, owner.username if owner else "")


@router.get("/collections/{collection_id}/items")
def list_public_collection_items(
    collection_id: int,
    page: int = Query(1, ge=1),
    page_size: int = Query(24, ge=1, le=100),
    db: Session = Depends(get_db),
    viewer: User | None = Depends(optional_user),
):
    c = db.get(Collection, collection_id)
    is_owner = c is not None and viewer is not None and c.owner_id == viewer.id
    if c is None or (c.visibility != "public" and not is_owner):
        raise HTTPException(status_code=404, detail="收藏夹不存在")
    return _page_items(db, c, page, page_size, viewer.id if viewer else None)


# ---------- 公共实现（两个 items 端点共用） ----------

def _add(db: Session, c: Collection, slug: str, user: User) -> None:
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="作品不存在")
    exists = (
        db.query(CollectionItem)
        .filter(CollectionItem.collection_id == c.id, CollectionItem.demo_id == demo.id)
        .first()
    )
    if exists is not None:
        return  # 幂等：重复加入静默成功
    count = db.query(func.count(CollectionItem.id)).filter(CollectionItem.collection_id == c.id).scalar() or 0
    if count >= MAX_ITEMS:
        raise HTTPException(status_code=400, detail=f"收藏夹已满（最多 {MAX_ITEMS} 件）")
    db.add(CollectionItem(collection_id=c.id, demo_id=demo.id))
    c.updated_at = utcnow()
    db.commit()


def _page_items(db: Session, c: Collection, page: int, page_size: int, viewer_id: int | None) -> CollectionItemPage:
    total = db.query(func.count(CollectionItem.id)).filter(CollectionItem.collection_id == c.id).scalar() or 0
    rows = (
        db.query(CollectionItem, Demo)
        .join(Demo, Demo.id == CollectionItem.demo_id)
        .filter(CollectionItem.collection_id == c.id)
        .order_by(CollectionItem.created_at.desc())
        .offset((page - 1) * page_size)
        .limit(page_size)
        .all()
    )
    demos = [d for _it, d in rows]
    from ..serializers import preload_demo_relations

    preload_demo_relations(db, demos)
    items = [
        {"demo": serialize_demo(db, d, current_user_id=viewer_id), "added_at": it.created_at}
        for it, d in rows
    ]
    return CollectionItemPage(items=items, total=int(total), page=page, page_size=page_size)
