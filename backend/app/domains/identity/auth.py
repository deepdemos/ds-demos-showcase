import asyncio

from fastapi import APIRouter, Depends, File, HTTPException, Request, Response, UploadFile
from sqlalchemy.orm import Session

from ...client_ip import get_client_ip
from ...config import settings
from ...database import get_db
from ...models import User
from ...schemas import AuthResponse, ChangePasswordRequest, LoginRequest, MeOut, MePatch, RegisterRequest, UserOut
from ...services import ratelimit, storage, storage
from .deps import current_user
from .security import (
    clear_auth_cookie,
    create_access_token,
    hash_password,
    set_auth_cookie,
    verify_password,
)

router = APIRouter(prefix="/auth", tags=["auth"])

# 登录限流（KB-23）：每 IP / 每用户名 5 分钟内最多 10 次尝试
LOGIN_RATE = 10
LOGIN_WINDOW = 300


@router.post("/register", status_code=201, response_model=AuthResponse)
def register(body: RegisterRequest, request: Request, response: Response, db: Session = Depends(get_db)):
    existing = db.query(User).filter(User.username == body.username).first()
    if existing:
        raise HTTPException(status_code=409, detail="用户名已存在", )
    user = User(username=body.username, password_hash=hash_password(body.password))
    db.add(user)
    db.commit()
    db.refresh(user)
    token = create_access_token(user.id)
    set_auth_cookie(request, response, token)
    return AuthResponse(access_token=token, user=UserOut.model_validate(user))


@router.post("/login", response_model=AuthResponse)
def login(body: LoginRequest, request: Request, response: Response, db: Session = Depends(get_db)):
    # KB-23：登录失败按 IP + 用户名双维度限流（旧实现可无限次爆破口令）
    ip = get_client_ip(request) or "unknown"
    ratelimit.hit(f"login:ip:{ip}", LOGIN_RATE, LOGIN_WINDOW)
    ratelimit.hit(f"login:user:{body.username.strip().lower()}", LOGIN_RATE, LOGIN_WINDOW)
    user = db.query(User).filter(User.username == body.username).first()
    if user is None or not verify_password(body.password, user.password_hash):
        raise HTTPException(status_code=401, detail="用户名或密码错误", )
    if user.status != "active":
        raise HTTPException(status_code=403, detail="账号不可用", )
    token = create_access_token(user.id)
    set_auth_cookie(request, response, token)
    return AuthResponse(access_token=token, user=UserOut.model_validate(user))


@router.post("/logout", status_code=204)
def logout(response: Response, user: User = Depends(current_user)):
    clear_auth_cookie(response)
    response.status_code = 204
    return response


@router.get("/me", response_model=MeOut)
def me(user: User = Depends(current_user)):
    return user


@router.patch("/me", response_model=MeOut)
def update_me(body: MePatch, db: Session = Depends(get_db), user: User = Depends(current_user)):
    """改展示名 / 简介 / 历史开关。

    history_enabled 只**停止后续记录**，不动已有数据 —— "关闭"与"删除"是两件事，
    界面上分开呈现（前端切到"不记录"后才提供"关闭并清空"）。
    """
    if body.display_name is not None:
        user.display_name = body.display_name.strip()[:64]
    if body.bio is not None:
        user.bio = body.bio.strip()[:500]
    if body.history_enabled is not None:
        user.history_enabled = bool(body.history_enabled)
    db.commit()
    db.refresh(user)
    return user


@router.post("/me/avatar")
async def upload_avatar(
    file: UploadFile = File(...),
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    """头像上传：端侧已先压成 512×512，这里**再归一化一次**（双保险，也兜住端侧解不了的格式）。

    接收上限与封面一致（settings.max_file_size），不做"图片太大"式的严苛限制。
    """
    from ...routers.demos import _read_limited

    data = await _read_limited(file, settings.max_file_size, "图片过大（上限 200MB）")
    old = user.avatar_url
    url = await asyncio.to_thread(storage.save_avatar, user.id, data)
    user.avatar_url = url
    db.commit()
    db.refresh(user)
    # 注意 old != url：文件名带内容哈希，同一张图重复上传会得到**同一个 URL**，
    # 此时删除等于把自己刚写好的文件删掉（实测踩到）。只在真的换了一张图时才清理旧文件。
    if old and old != url and old.startswith("/media/avatars/"):
        try:
            await asyncio.to_thread(storage.delete_media_file, old)
        except Exception:  # noqa: BLE001 —— 删旧文件失败不该让"换头像"失败
            pass
    return {"avatar_url": url}


@router.delete("/me/avatar", status_code=204)
def remove_avatar(db: Session = Depends(get_db), user: User = Depends(current_user)):
    old = user.avatar_url
    user.avatar_url = ""
    db.commit()
    if old and old.startswith("/media/avatars/"):
        try:
            storage.delete_media_file(old)
        except Exception:  # noqa: BLE001
            pass
    return None


@router.post("/change-password", status_code=204)
def change_password(body: ChangePasswordRequest, db: Session = Depends(get_db), user: User = Depends(current_user)):
    if not verify_password(body.old_password, user.password_hash):
        raise HTTPException(status_code=401, detail="原密码错误", )
    user.password_hash = hash_password(body.new_password)
    db.commit()
    return Response(status_code=204)
