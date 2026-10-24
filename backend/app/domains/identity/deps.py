from fastapi import Depends, HTTPException, Request
from sqlalchemy.orm import Session

from ...database import get_db
from ...models import User
from .security import decode_access_token, token_from_request


def current_user(request: Request, db: Session = Depends(get_db)) -> User:
    token = token_from_request(request)
    user_id = decode_access_token(token) if token else None
    if user_id is None:
        raise HTTPException(status_code=401, detail="未登录或登录已过期", )
    user = db.get(User, user_id)
    if user is None:
        raise HTTPException(status_code=401, detail="用户不存在", )
    if user.status != "active":
        raise HTTPException(status_code=403, detail="账号不可用", )
    return user


def optional_user(request: Request, db: Session = Depends(get_db)) -> User | None:
    token = token_from_request(request)
    user_id = decode_access_token(token) if token else None
    if user_id is None:
        return None
    user = db.get(User, user_id)
    if user is None or user.status != "active":
        return None
    return user


def require_admin(user: User = Depends(current_user)) -> User:
    if user.role != "admin":
        raise HTTPException(status_code=403, detail="需要管理员权限")
    return user
