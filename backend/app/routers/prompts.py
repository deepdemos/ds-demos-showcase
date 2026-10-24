"""提示词库：公开浏览与精确正文对照，独立投稿复用知识收件箱审核。"""

import json

from fastapi import APIRouter, Depends, HTTPException, Query, Response
from sqlalchemy.orm import Session

from ..database import get_db
from ..domains.identity.deps import current_user
from ..models import EntitySuggestion, Prompt, User
from ..schemas import Paginated, PromptListOut, PromptProposalIn, PromptProposalOut, PromptSummaryOut
from ..services import prompt_library_service, ratelimit, suggestion_service
from ..services.scope import get_scope

router = APIRouter(prefix="/prompts", tags=["prompts"])
PROMPT_PROPOSE_RATE_LIMIT = 10


@router.get("", response_model=PromptListOut)
def list_prompts(
    q: str | None = Query(default=None, max_length=2000),
    category: str | None = Query(default=None, max_length=64),
    sort: str = Query(default="newest", pattern="^(newest|most_tested)$"),
    has_results: bool | None = None,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
):
    items, total = prompt_library_service.list_prompts(
        db, scope=scope, q=q, category=category, sort=sort,
        has_results=has_results, page=page, page_size=page_size,
    )
    return PromptListOut(items=items, total=total, page=page, page_size=page_size)


@router.post("", status_code=202, response_model=PromptProposalOut, response_model_exclude_none=True)
def propose_prompt(
    payload: PromptProposalIn,
    response: Response,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
    scope: str = Depends(get_scope),
):
    values = prompt_library_service.validate_proposal(payload.model_dump())
    ratelimit.hit(f"prompt:propose:{user.id}", PROMPT_PROPOSE_RATE_LIMIT, 3600)
    prompt_library_service.lock_submission(db, user.id)
    digest = prompt_library_service.content_hash(values["content"])
    existing = db.query(Prompt).filter(Prompt.content_hash == digest).first()
    if existing is not None and prompt_library_service.get_prompt(db, existing.id, scope=scope) is not None:
        response.status_code = 200
        return PromptProposalOut(status="existing", prompt_id=existing.id)

    # 收件箱默认定位键不含正文；这里按“同人 + 同视区 + 同正文”去重。
    suggestions = (db.query(EntitySuggestion)
                   .filter(EntitySuggestion.kind == "new_prompt", EntitySuggestion.status == "pending",
                           EntitySuggestion.created_by == user.id).all())
    for suggestion in suggestions:
        try:
            previous = json.loads(suggestion.payload or "{}")
        except json.JSONDecodeError:
            continue
        if (isinstance(previous, dict) and previous.get("scope", "deep") == scope
                and previous.get("content") == values["content"]):
            return PromptProposalOut(status="pending", suggestion_id=suggestion.id)
    suggestion = suggestion_service.create(
        db, kind="new_prompt", payload={**values, "scope": scope},
        confidence=0.98, source="user", created_by=user.id, dedupe=False,
    )
    if suggestion is None:
        raise HTTPException(status_code=500, detail="提示词投稿失败，请重试")
    db.commit()
    return PromptProposalOut(status="pending", suggestion_id=suggestion.id)


@router.get("/{prompt_id}", response_model=PromptSummaryOut)
def prompt_detail(
    prompt_id: int,
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
):
    summary = prompt_library_service.get_prompt(db, prompt_id, scope=scope)
    if summary is None:
        raise HTTPException(status_code=404, detail="提示词不存在或尚未公开")
    return PromptSummaryOut(**summary)


@router.get("/{prompt_id}/demos", response_model=Paginated)
def prompt_demos(
    prompt_id: int,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
):
    if prompt_library_service.get_prompt(db, prompt_id, scope=scope) is None:
        raise HTTPException(status_code=404, detail="提示词不存在或尚未公开")
    items, total = prompt_library_service.list_prompt_demos(db, prompt_id, scope=scope, page=page, page_size=page_size)
    return Paginated(items=items, total=total, page=page, page_size=page_size)
