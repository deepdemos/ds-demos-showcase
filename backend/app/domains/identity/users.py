from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy.orm import Session

from ...database import get_db
from ...models import Demo, User
from ...schemas import FollowOut, UserLeaderboardPage, UserPatch, UserProfileOut, UserPublic
from ...services import audit_service, community_service
from .deps import current_user, optional_user, require_admin

router = APIRouter(tags=["users"])


@router.get("/users/leaderboard", response_model=UserLeaderboardPage)
def user_leaderboard(
    sort: str = Query(default="reputation", pattern="^(reputation|likes|thanks|topics|replies|demos|followers)$"),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
):
    return community_service.user_leaderboard(db, sort, page, page_size)


def _user_public(db: Session, user: User) -> UserPublic:
    demo_count = db.query(Demo).filter(Demo.author_id == user.id).count()
    return UserPublic(
        id=user.id,
        username=user.username,
        role=user.role,
        status=user.status,
        bio=user.bio,
        # 展示名与头像：空则前端回落 username / 首字母方块
        display_name=user.display_name or "",
        avatar_url=user.avatar_url or "",
        created_at=user.created_at,
        demo_count=demo_count,
    )


@router.get("/users/{username}", response_model=UserPublic)
def get_user(username: str, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.username == username).first()
    if user is None or user.status == "deleted":
        raise HTTPException(status_code=404, detail="用户不存在")
    return _user_public(db, user)


@router.get("/users/{username}/profile", response_model=UserProfileOut)
def get_user_profile(
    username: str,
    db: Session = Depends(get_db),
    viewer: User | None = Depends(optional_user),
):
    return community_service.user_profile(db, username, viewer.id if viewer else None)


@router.post("/users/{user_id}/follow", response_model=FollowOut)
def toggle_follow(
    user_id: int,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    return community_service.toggle_follow(db, user, user_id)


@router.get("/users/{username}/followers", response_model=list[UserPublic])
def list_followers(username: str, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.username == username).first()
    if user is None or user.status == "deleted":
        raise HTTPException(status_code=404, detail="用户不存在")
    return [_user_public(db, u) for u in community_service.list_followers(db, user.id)]


@router.get("/users/{username}/following", response_model=list[UserPublic])
def list_following(username: str, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.username == username).first()
    if user is None or user.status == "deleted":
        raise HTTPException(status_code=404, detail="用户不存在")
    return [_user_public(db, u) for u in community_service.list_following(db, user.id)]


@router.patch("/users/{user_id}", response_model=UserPublic)
def patch_user(user_id: int, body: UserPatch, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """改用户角色/状态（KB-17：改角色与封禁都是治理动作，同事务落审计）。"""
    user = db.get(User, user_id)
    if user is None:
        raise HTTPException(status_code=404, detail="用户不存在")
    before = {"username": user.username, "role": user.role, "status": user.status}
    if body.role is not None:
        user.role = body.role
    if body.status is not None:
        user.status = body.status
    if before != {"username": user.username, "role": user.role, "status": user.status}:
        audit_service.record(
            db,
            action="status_set" if body.status is not None else "update",
            entity_type="user",
            entity_id=user.id,
            actor_id=admin.id,
            before=before,
            after={"username": user.username, "role": user.role, "status": user.status},
            reason=f"管理端调整用户 {user.username}：role={user.role} status={user.status}",
        )
    db.commit()
    db.refresh(user)
    return _user_public(db, user)
