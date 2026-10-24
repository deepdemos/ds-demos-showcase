"""identity 域的对外入口：main.py 只装配这一个 router。

域内路由各自声明 prefix/tags；聚合器只负责按既有顺序拼接，
保证 `main.py` 不需要知道 identity 内部有几组端点。
"""

from fastapi import APIRouter

from . import auth, users

router = APIRouter()
router.include_router(auth.router)
router.include_router(users.router)
