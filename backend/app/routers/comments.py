from fastapi import APIRouter, Depends, HTTPException, Response
from sqlalchemy.orm import Session

from ..database import get_db
from ..domains.identity.deps import current_user
from ..models import Comment, Demo, User
from ..schemas import CommentCreate, CommentOut

router = APIRouter(tags=["comments"])

MAX_DEPTH = 5


def _find_demo(db: Session, slug: str) -> Demo:
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    return demo


def _depth_of(db: Session, comment_id: int) -> int:
    depth = 1
    current = db.get(Comment, comment_id)
    while current is not None and current.parent_id is not None:
        depth += 1
        if depth > MAX_DEPTH:
            break
        current = db.get(Comment, current.parent_id)
    return depth


def _build_tree(comments: list[Comment]) -> list[CommentOut]:
    nodes: dict[int, CommentOut] = {}
    for c in comments:
        nodes[c.id] = CommentOut(
            id=c.id,
            demo_id=c.demo_id,
            user_id=c.user_id,
            username=c.user.username if c.user else None,
            parent_id=c.parent_id,
            content=c.content,
            created_at=c.created_at,
            children=[],
        )
    roots: list[CommentOut] = []
    for node in nodes.values():
        if node.parent_id is not None and node.parent_id in nodes:
            nodes[node.parent_id].children.append(node)
        else:
            roots.append(node)
    return roots


@router.get("/demos/{slug}/comments", response_model=list[CommentOut])
def list_comments(slug: str, db: Session = Depends(get_db)):
    _find_demo(db, slug)
    comments = (
        db.query(Comment)
        .join(Demo, Comment.demo_id == Demo.id)
        .filter(Demo.slug == slug)
        .order_by(Comment.created_at.asc())
        .all()
    )
    return _build_tree(comments)


@router.post("/demos/{slug}/comments", status_code=410)
def create_comment(slug: str, body: CommentCreate, db: Session = Depends(get_db), user: User = Depends(current_user)):
    """评论已迁移到论坛：写操作下线，返回 410。"""
    raise HTTPException(status_code=410, detail="评论已迁移到论坛讨论，请使用 /forum/topics", )


@router.delete("/comments/{comment_id}", status_code=410)
def delete_comment(comment_id: int, db: Session = Depends(get_db), user: User = Depends(current_user)):
    """评论已迁移到论坛：写操作下线，返回 410。"""
    raise HTTPException(status_code=410, detail="评论已迁移到论坛讨论，请在论坛管理处理", )