package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 批准「收录模型」提案（#593 / 模型域 §6.7）。
//
// # 核心语义：批准 ≠ 可用
//
// 批准一条 new_model 提案只会建出 status = 'candidate'、resolution = 'exact' 的候选模型，
// 并复用手动新建（model_write.go 的 createModelInTx）完成查重、建行与 create 审计——全仓不写
// 第二份建模型逻辑。若提案携带上游三元组（source / provider_id / external_id），同一事务内
// 当场写入 model_offerings 完成认领；若三元组已被占用或模型名 / slug 冲突，整笔事务回滚
// 并返回 409 指明归属。候选模型需经后续治理确认（model.govern）才变为 active。

const (
	newModelProposalNameMax       = 128
	newModelProposalVendorMax     = 64
	newModelProposalProviderIDMax = 128
	newModelProposalExternalIDMax = 256
)

type newModelProposalPayload struct {
	Name        string
	Vendor      string
	Source      string
	ProviderID  string
	ExternalID  string
	HasUpstream bool
}

func payloadStringField(payload map[string]any, key, label string, maxRunes int) (string, *proposalFailure) {
	raw, exists := payload[key]
	if !exists || raw == nil {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, label+"必须是字符串")
	}
	if strings.ContainsRune(s, 0) {
		return "", proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, label+"包含非法空字符（NUL）")
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) > maxRunes {
		return "", proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
			fmt.Sprintf("%s最多 %d 个字", label, maxRunes))
	}
	return s, nil
}

// parseNewModelProposalPayload 校验并提取 new_model 提案的 payload。
func parseNewModelProposalPayload(raw []byte) (newModelProposalPayload, *proposalFailure) {
	payload := proposalPayloadMap(raw)

	name, fail := payloadStringField(payload, "name", "new_model 的名称", newModelProposalNameMax)
	if fail != nil {
		return newModelProposalPayload{}, fail
	}
	if name == "" {
		return newModelProposalPayload{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "new_model 的名称不能为空")
	}
	if model.FallbackModelName(name) {
		return newModelProposalPayload{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
			"「"+name+"」是系统维护的兜底名（表示说不清具体型号），不能建为模型")
	}

	vendor, fail := payloadStringField(payload, "vendor", "厂商名", newModelProposalVendorMax)
	if fail != nil {
		return newModelProposalPayload{}, fail
	}
	source, fail := payloadStringField(payload, "source", "上游来源（source）", newModelProposalVendorMax)
	if fail != nil {
		return newModelProposalPayload{}, fail
	}
	providerID, fail := payloadStringField(payload, "provider_id", "上游服务方（provider_id）", newModelProposalProviderIDMax)
	if fail != nil {
		return newModelProposalPayload{}, fail
	}
	externalID, fail := payloadStringField(payload, "external_id", "上游模型标识（external_id）", newModelProposalExternalIDMax)
	if fail != nil {
		return newModelProposalPayload{}, fail
	}

	hasProvider := providerID != ""
	hasExternal := externalID != ""
	switch {
	case hasProvider != hasExternal:
		return newModelProposalPayload{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
			"上游三元组不完整（provider_id 与 external_id 必须同时存在）")
	case hasProvider && hasExternal:
		if source == "" {
			source = catalogSource
		}
		if source != catalogSource {
			return newModelProposalPayload{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
				fmt.Sprintf("不支持的上游目录来源 %q（当前仅支持 %s）", source, catalogSource))
		}
		return newModelProposalPayload{
			Name:        name,
			Vendor:      vendor,
			Source:      source,
			ProviderID:  providerID,
			ExternalID:  externalID,
			HasUpstream: true,
		}, nil
	case source != "":
		return newModelProposalPayload{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
			"提案只给了 source，缺少 provider_id 与 external_id")
	default:
		return newModelProposalPayload{
			Name:   name,
			Vendor: vendor,
		}, nil
	}
}

// offeringClaimConflictMessage 把「三元组已被认领」翻成带归属链接的 409 文案（模型域 §6.7.4）。
func offeringClaimConflictMessage(owner store.FindOfferingOwnerRow) string {
	if owner.Status == "deprecated" && owner.MergedIntoSlug != "" {
		return fmt.Sprintf("该上游条目已被已退役模型「%s」（/models/%s）认领，请查看其合并归宿 /models/%s",
			owner.Name, owner.Slug, owner.MergedIntoSlug)
	}
	return fmt.Sprintf("该上游条目已被模型「%s」（/models/%s，状态 %s）认领，不能重复认领",
		owner.Name, owner.Slug, owner.Status)
}

// approveNewModelProposal 执行 new_model 提案批准：锁提案 → 校验与查重 → 复用 createModelInTx 建候选
// →（如有上游三元组）当场认领 model_offerings → 回填提案 result_kind='model'/result_id → 写审计。
func (h *Handler) approveNewModelProposal(ctx context.Context, actor demo.Actor, id int64) (api.ProposalReviewResult, *proposalFailure) {
	if h.pool == nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := h.queries.WithTx(tx)

	locked, err := q.LockPendingProposalForModel(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
	}
	if err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}

	parsed, failure := parseNewModelProposalPayload(locked.Payload)
	if failure != nil {
		return api.ProposalReviewResult{}, failure
	}

	vendor := parsed.Vendor
	var catalogItem store.GetCatalogItemRow
	if parsed.HasUpstream {
		item, err := q.GetCatalogItem(ctx, store.GetCatalogItemParams{
			Source:     parsed.Source,
			ProviderID: parsed.ProviderID,
			ExternalID: parsed.ExternalID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
				"上游目录镜像里找不到该条目，请先同步目录或驳回提案")
		}
		if err != nil {
			return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		}
		catalogItem = item

		owner, err := q.FindOfferingOwner(ctx, store.FindOfferingOwnerParams{
			Authority:  parsed.Source,
			ProviderID: parsed.ProviderID,
			ExternalID: parsed.ExternalID,
		})
		switch {
		case err == nil:
			return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, offeringClaimConflictMessage(owner))
		case !errors.Is(err, pgx.ErrNoRows):
			return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		}

		if vendor == "" && catalogItem.VendorSlug.Valid && catalogItem.VendorSlug.String != "" {
			vendor = catalogItem.VendorSlug.String
		}
	}

	slug := model.Slug(parsed.Name, vendor)

	// 复用手动新建的唯一建模型逻辑（model_write.go 的 createModelInTx）：
	// 查重、建行（resolution='exact'）与 model:create 审计完全同源，只把状态设为 candidate。
	modelID, err := h.createModelInTx(ctx, q, actor, modelCreate{
		Name:   parsed.Name,
		Slug:   slug,
		Vendor: vendor,
		Status: "candidate",
	})
	if err != nil {
		var conflict *modelConflict
		if errors.As(err, &conflict) {
			return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, modelConflictMessage(conflict))
		}
		if status, code, msg, ok := modelFieldViolation(err); ok {
			if status == http.StatusConflict {
				if existing, lookErr := h.queries.FindModelByNameOrSlug(ctx, store.FindModelByNameOrSlugParams{
					Name: parsed.Name,
					Slug: slug,
				}); lookErr == nil {
					return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict,
						modelConflictMessage(&modelConflict{existing: existing, sameName: existing.Name == parsed.Name}))
				}
				return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, msg)
			}
			return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, code, msg)
		}
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}

	if parsed.HasUpstream {
		if err := q.InsertModelOfferingStrict(ctx, store.InsertModelOfferingStrictParams{
			ModelID:      modelID,
			Authority:    parsed.Source,
			ProviderID:   parsed.ProviderID,
			ProviderName: catalogItem.ProviderName,
			ExternalID:   parsed.ExternalID,
			CanonicalID:  catalogItem.CanonicalID,
			IsOfficial:   parsed.ProviderID == vendorPrefix(catalogItem.CanonicalID.String),
		}); err != nil {
			if isUniqueViolation(err) {
				if owner, lookErr := h.queries.FindOfferingOwner(ctx, store.FindOfferingOwnerParams{
					Authority:  parsed.Source,
					ProviderID: parsed.ProviderID,
					ExternalID: parsed.ExternalID,
				}); lookErr == nil {
					return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, offeringClaimConflictMessage(owner))
				}
				return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "该上游条目已被其他模型认领")
			}
			return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		}
	}

	if _, err := q.MarkProposalApproved(ctx, store.MarkProposalApprovedParams{
		ID:         id,
		ResultKind: pgtype.Text{String: "model", Valid: true},
		ResultID:   pgtype.Int8{Int64: modelID, Valid: true},
		ReviewedBy: pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
		}
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}

	// 提案审核审计放在同一事务里（实体域 §4.10 KB-16）：
	// 模型行、认领行、提案状态、model:create 与 proposal:review 五者同生共死。
	beforeJSON, err := json.Marshal(map[string]string{"status": locked.Status})
	if err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
	afterJSON, err := json.Marshal(map[string]any{
		"status":      "approved",
		"result_kind": "model",
		"result_id":   modelID,
	})
	if err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
	if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorID:    pgtype.Int8{Int64: actor.AccountID, Valid: actor.AccountID != 0},
		ActorRole:  actor.Role,
		Action:     "review",
		EntityType: "proposal",
		EntityID:   id,
		Before:     beforeJSON,
		After:      afterJSON,
		Reason:     "",
	}); err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}

	if err := tx.Commit(ctx); err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
	return api.ProposalReviewResult{Id: int(id), Status: api.ProposalReviewResultStatusApproved}, nil
}
