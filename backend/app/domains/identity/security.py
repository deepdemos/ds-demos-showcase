import base64
import hashlib
import hmac
import os
from datetime import datetime, timedelta, timezone

import jwt
from fastapi import Request

from ...config import settings

COOKIE_NAME = "demo_token"
ALGORITHM = settings.jwt_algorithm


def hash_password(password: str) -> str:
    salt = os.urandom(16)
    dk = hashlib.pbkdf2_hmac("sha256", password.encode("utf-8"), salt, 120_000)
    return "pbkdf2$" + base64.b64encode(salt).decode() + "$" + base64.b64encode(dk).decode()


def verify_password(password: str, encoded: str) -> bool:
    try:
        _, salt_b64, dk_b64 = encoded.split("$", 2)
        salt = base64.b64decode(salt_b64)
        expected = base64.b64decode(dk_b64)
    except Exception:
        return False
    dk = hashlib.pbkdf2_hmac("sha256", password.encode("utf-8"), salt, 120_000)
    return hmac.compare_digest(dk, expected)


def create_access_token(user_id: int) -> str:
    now = datetime.now(timezone.utc)
    expire = now + timedelta(minutes=settings.access_token_expire_minutes)
    payload = {"sub": str(user_id), "exp": expire, "iat": now}
    return jwt.encode(payload, settings.jwt_secret, algorithm=ALGORITHM)


def decode_access_token(token: str) -> int | None:
    try:
        payload = jwt.decode(token, settings.jwt_secret, algorithms=[ALGORITHM])
        return int(payload["sub"])
    except Exception:
        return None


def token_from_request(request: Request) -> str | None:
    auth = request.headers.get("Authorization", "")
    if auth.lower().startswith("bearer "):
        return auth[7:].strip()
    return request.cookies.get(COOKIE_NAME)


def set_auth_cookie(request: Request, response, token: str) -> None:
    # HTTPS 终止在反代/CDN（如 Cloudflare）时 request.url.scheme 恒为 http，
    # 需 COOKIE_SECURE=true 或按 X-Forwarded-Proto 判定，Cookie 才能带上 Secure 标志
    secure = (
        settings.cookie_secure
        or request.url.scheme == "https"
        or request.headers.get("x-forwarded-proto", "") == "https"
    )
    response.set_cookie(
        COOKIE_NAME,
        token,
        max_age=settings.access_token_expire_minutes * 60,
        httponly=True,
        secure=secure,
        samesite="lax",
        path="/",
    )


def clear_auth_cookie(response) -> None:
    response.delete_cookie(COOKIE_NAME, path="/")
