from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.responses import JSONResponse, PlainTextResponse

from ..config import settings
from ..domains.identity.deps import optional_user
from ..models import User
from ..services import site_info_service
from ..services.scope import get_scope

router = APIRouter(prefix="/meta", tags=["meta"])


@router.get("/agent-guide", response_class=PlainTextResponse)
def agent_guide():
    """AI agent 上传指南全文（AI_AGENT_GUIDE.md），供 agent 直接爬取。"""
    path = settings.site_repo_path / "AI_AGENT_GUIDE.md"
    if not path.exists():
        raise HTTPException(status_code=404, detail="AI_AGENT_GUIDE.md 未找到", )
    return PlainTextResponse(path.read_text(encoding="utf-8"), media_type="text/markdown; charset=utf-8")


@router.get("/site-info")
def site_info(request: Request, refresh: int = 0, admin: User | None = Depends(optional_user)):
    """站点公开概况：内容/社区/流量/热门/能力（机器可读，agent 与前端共用）。

    60s 内存缓存 + CDN 可缓存（max-age=60）；仅 admin 可 ?refresh=1 强刷。
    只含公开安全数字——待审队列/存储等管理面信息在 /admin/stats。
    """
    force = bool(refresh) and admin is not None and admin.role == "admin"
    # 按请求视区聚合（astra 橱窗与 deep 主站数据面/缓存独立，见 site_info_service）
    data = site_info_service.get_site_info(force=force, scope=get_scope(request))
    return JSONResponse(data, headers={"Cache-Control": "public, max-age=60"})
