package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ModelProposal 是作者在投稿 / 编辑时顺带提交的一条「收录模型」提案（#590）。
//
// 为什么随投稿 / 编辑一起提交、不在通用 POST /api/v1/proposals 开放：
// 收录提案的全部价值在于「有作品要用它」，必须关联一件具体作品（proposal_refs role='subject'）。
// 与 ModelCall 采用同一套 multipart 重复 JSON 字符串编码（FormData 只能传字符串）。
type ModelProposal struct {
	Name       string `json:"name"`
	Vendor     string `json:"vendor,omitempty"`
	Source     string `json:"source,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
}

const (
	// maxModelProposals 是一次投稿 / 编辑最多同时提出的未入库模型提案条数。
	maxModelProposals = 20

	// maxProposalJSONLen 是单条提案 JSON 字符串的长度上限（与契约 maxLength 对齐）。
	maxProposalJSONLen = 2048

	// maxProposalVendorLen 与 models.vendor 的长度约束（64 字）对齐。
	maxProposalVendorLen = 64

	// 上游三元组各段长度上限（与 model_offerings 约束一致）。
	maxProposalSourceLen     = 64
	maxProposalProviderIDLen = 128
	maxProposalExternalIDLen = 256

	defaultProposalUpstreamSource = "models.dev"
)

// parseModelProposals 把 multipart 里重复出现的 modelProposals 字段解成结构。
//
// 严格使用 DisallowUnknownFields 并拒收尾部残留内容：拼错字段名（如 provider_id 写成别的名字）
// 绝不能静默丢弃，否则审核员收到残缺提案、作者却以为上游三元组已经带上了。
func parseModelProposals(raw []string) ([]ModelProposal, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxModelProposals {
		return nil, fmt.Errorf("%w: 模型收录提案最多 %d 条，这次给了 %d 条", ErrInvalidInput, maxModelProposals, len(raw))
	}
	out := make([]ModelProposal, 0, len(raw))
	for i, s := range raw {
		if !utf8.ValidString(s) {
			return nil, fmt.Errorf("%w: 第 %d 条模型收录提案不是合法的 UTF-8 文本", ErrInvalidInput, i+1)
		}
		if n := utf8.RuneCountInString(s); n > maxProposalJSONLen {
			return nil, fmt.Errorf("%w: 单条模型收录提案最长 %d 个字符，这条有 %d 个", ErrInvalidInput, maxProposalJSONLen, n)
		}
		var p ModelProposal
		dec := json.NewDecoder(strings.NewReader(s))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("%w: 第 %d 条模型收录提案不是合法的 JSON 对象：%v", ErrInvalidInput, i+1, err)
		}
		if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
			return nil, fmt.Errorf("%w: 第 %d 条模型收录提案里只能有一个 JSON 对象（后面还有内容）", ErrInvalidInput, i+1)
		}
		out = append(out, p)
	}
	return out, nil
}

// preparedModelProposal 是校验、规整与单请求内去重之后的提案。
type preparedModelProposal struct {
	Name           string
	NormalizedName string
	Vendor         string
	Source         string
	ProviderID     string
	ExternalID     string
}

// normalizeProposalModelName 把模型名折成去重键用的规范化写法：折叠连续空白并转小写。
func normalizeProposalModelName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// prepareModelProposals 校验字段完整性与长度，并在单次请求内按规范化模型名去重。
func prepareModelProposals(items []ModelProposal) ([]preparedModelProposal, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]preparedModelProposal, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: 第 %d 条模型收录提案缺少模型名称（name）", ErrInvalidInput, i+1)
		}
		if len([]rune(name)) > maxModelNameLen {
			return nil, fmt.Errorf("%w: 模型收录提案的名称最长 %d 个字：%s", ErrInvalidInput, maxModelNameLen, abbreviate(name))
		}
		vendor := strings.TrimSpace(item.Vendor)
		if len([]rune(vendor)) > maxProposalVendorLen {
			return nil, fmt.Errorf("%w: 模型收录提案的厂商名最长 %d 个字：%s", ErrInvalidInput, maxProposalVendorLen, abbreviate(vendor))
		}
		source := strings.TrimSpace(item.Source)
		providerID := strings.TrimSpace(item.ProviderID)
		externalID := strings.TrimSpace(item.ExternalID)

		hasProvider := providerID != ""
		hasExternal := externalID != ""
		switch {
		case hasProvider != hasExternal:
			return nil, fmt.Errorf("%w: 第 %d 条模型收录提案的上游标识不完整（providerId 与 externalId 必须同时提供）", ErrInvalidInput, i+1)
		case hasProvider && hasExternal:
			if source == "" {
				source = defaultProposalUpstreamSource
			}
			if len([]rune(source)) > maxProposalSourceLen {
				return nil, fmt.Errorf("%w: 上游来源（source）最长 %d 个字符", ErrInvalidInput, maxProposalSourceLen)
			}
			if len([]rune(providerID)) > maxProposalProviderIDLen {
				return nil, fmt.Errorf("%w: 上游服务方（providerId）最长 %d 个字符", ErrInvalidInput, maxProposalProviderIDLen)
			}
			if len([]rune(externalID)) > maxProposalExternalIDLen {
				return nil, fmt.Errorf("%w: 上游模型标识（externalId）最长 %d 个字符", ErrInvalidInput, maxProposalExternalIDLen)
			}
		case source != "":
			return nil, fmt.Errorf("%w: 第 %d 条模型收录提案只给了 source，缺少 providerId 与 externalId", ErrInvalidInput, i+1)
		}

		norm := normalizeProposalModelName(name)
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, preparedModelProposal{
			Name:           name,
			NormalizedName: norm,
			Vendor:         vendor,
			Source:         source,
			ProviderID:     providerID,
			ExternalID:     externalID,
		})
	}
	return out, nil
}

// validateModelProposals 在落盘 / 上传之前先挡掉不合格的提案输入，避免 200 MiB 上传白跑。
func validateModelProposals(raw []string) ([]preparedModelProposal, error) {
	parsed, err := parseModelProposals(raw)
	if err != nil {
		return nil, err
	}
	return prepareModelProposals(parsed)
}

// applyModelProposals 在投稿 / 编辑的同一事务里写入 new_model 提案与 proposal_refs(role='subject')。
//
// 去重单位是「作品 × 规范化模型名」（dedupe_key = "<demoID>:<normalizedName>"）：
// 同一作品重复提同一模型时，uq_proposals_pending_dedupe 命中 ON CONFLICT DO NOTHING，
// 静默跳过不产生第二条 pending 提案；不同作品各自生成独立提案（批准后各自挂到对应作品）。
func applyModelProposals(
	ctx context.Context,
	q *store.Queries,
	demoID int64,
	demoSlug string,
	revNo int,
	authorID int64,
	raw []string,
) ([]int64, error) {
	prepared, err := validateModelProposals(raw)
	if err != nil {
		return nil, err
	}
	if len(prepared) == 0 {
		return nil, nil
	}

	createdIDs := make([]int64, 0, len(prepared))
	for _, p := range prepared {
		payloadMap := map[string]any{
			"name":        p.Name,
			"demo_id":     demoID,
			"demo_slug":   demoSlug,
			"revision_no": revNo,
		}
		if p.Vendor != "" {
			payloadMap["vendor"] = p.Vendor
		}
		if p.ProviderID != "" {
			payloadMap["source"] = p.Source
			payloadMap["provider_id"] = p.ProviderID
			payloadMap["external_id"] = p.ExternalID
		}
		encoded, err := json.Marshal(payloadMap)
		if err != nil {
			return nil, fmt.Errorf("序列化模型收录提案失败: %w", err)
		}

		dedupeKey := fmt.Sprintf("%d:%s", demoID, p.NormalizedName)
		id, err := q.InsertModelProposal(ctx, store.InsertModelProposalParams{
			Payload:   encoded,
			CreatedBy: int8(authorID),
			DedupeKey: text(dedupeKey),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// 本作品已有同名的待审提案：幂等跳过，不阻断作品提交。
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("写入模型收录提案失败: %w", err)
		}
		if err := q.InsertProposalSubjectDemoRef(ctx, store.InsertProposalSubjectDemoRefParams{
			ProposalID: id,
			DemoID:     int8(demoID),
		}); err != nil {
			return nil, fmt.Errorf("写入模型收录提案来源作品关联失败: %w", err)
		}
		createdIDs = append(createdIDs, id)
	}
	return createdIDs, nil
}
