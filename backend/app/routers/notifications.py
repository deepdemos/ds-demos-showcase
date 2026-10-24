"""站内通知：列表 / 未读数 / 已读。"""

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy.orm import Session, joinedload

from ..database import get_db
from ..domains.identity.deps import current_user
from ..models import Notification, User
from ..schemas import NotificationOut, NotificationReadIn, UnreadCountOut

router = APIRouter(prefix="/notifications", tags=["notifications"])


def _notif_out(n: Notification) -> NotificationOut:
    return NotificationOut(
        id=n.id,
        type=n.type,
        actor=n.actor.username if n.actor else None,
        actor_id=n.actor_id,
        demo_slug=n.demo_slug,
        topic_id=n.topic_id,
        reply_id=n.reply_id,
        read=n.read,
        created_at=n.created_at,
    )


@router.get("", response_model=list[NotificationOut])
def list_notifications(
    unread_only: bool = Query(False),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    q = db.query(Notification).filter(Notification.user_id == user.id)
    if unread_only:
        q = q.filter(Notification.read == False)  # noqa: E712
    rows = (
        q.options(joinedload(Notification.actor))  # KB-19：一次 join 取 actor，避免逐行懒加载
        .order_by(Notification.created_at.desc(), Notification.id.desc())
        .offset((page - 1) * page_size)
        .limit(page_size)
        .all()
    )
    return [_notif_out(n) for n in rows]


@router.get("/unread-count", response_model=UnreadCountOut)
def unread_count(db: Session = Depends(get_db), user: User = Depends(current_user)):
    count = db.query(Notification).filter(Notification.user_id == user.id, Notification.read == False).count()  # noqa: E712
    return UnreadCountOut(count=count)


@router.post("/read", response_model=NotificationOut)
def mark_read(body: NotificationReadIn, db: Session = Depends(get_db), user: User = Depends(current_user)):
    n = db.get(Notification, body.id)
    if n is None or n.user_id != user.id:
        raise HTTPException(status_code=404, detail="通知不存在", )
    n.read = True
    db.commit()
    db.refresh(n)
    return _notif_out(n)


@router.post("/read-all", status_code=204)
def mark_all_read(db: Session = Depends(get_db), user: User = Depends(current_user)):
    db.query(Notification).filter(Notification.user_id == user.id, Notification.read == False).update(  # noqa: E712
        {Notification.read: True}, synchronize_session=False
    )
    db.commit()
    return None
