package model

import (
	"encoding/json"
	"fmt"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/catalog"
)

// upsertRow 是 `UpsertCatalogItems` 的 payload 元素：**字段名就是列名**。
//
// `jsonb_populate_recordset(NULL::model_catalog_items, payload)` 按名字把 JSON 摊成行，
// 多余的键忽略、缺的键给 NULL。所以这个结构体不写 json tag 就得跟 DDL 一字不差，
// 加列时这里也必须跟上——漏一列不会报错，只会让那一列永远写着 NULL。
//
// 为什么另起一个结构体，而不是直接序列化生成物里的 Params：可空列在生成物里是
// `pgtype.Text` 这类结构体，序列化出来是 `{"String":"","Valid":false}` 对象，
// 摊不成 SQL 的 NULL。指针才是 JSON 能表达「没有」的形状。
//
// 可空的六列加上 release_date 用指针，空串要变成 null：
//   - 空串落进可空文本列之后，「这条没有规范名」与「这条的规范名是空字符串」就成了
//     两件事，而只有前者是真的；
//   - release_date 更硬：空串喂给 date 列会让整次同步报错回滚。catalog.Date 已经把
//     畸形日期规整成空串，这里再把它变成 NULL，坏日期就只是「这一天不知道」。
//
// 反过来，那四个 NOT NULL 且默认空串的列（description / modalities_* / raw_json）保持
// 字符串：那几列的空串本来就是「上游给了空值」的既定落法，改了会让既有行无谓地动一遍。
type upsertRow struct {
	ProviderID               string   `json:"provider_id"`
	ProviderName             string   `json:"provider_name"`
	ExternalID               string   `json:"external_id"`
	Name                     string   `json:"name"`
	CanonicalID              *string  `json:"canonical_id"`
	VendorSlug               *string  `json:"vendor_slug"`
	Family                   *string  `json:"family"`
	Description              string   `json:"description"`
	ModalitiesIn             string   `json:"modalities_in"`
	ModalitiesOut            string   `json:"modalities_out"`
	ContextWindow            *int32   `json:"context_window"`
	MaxOutput                *int32   `json:"max_output"`
	CostIn                   *float64 `json:"cost_in"`
	CostOut                  *float64 `json:"cost_out"`
	ReleaseDate              *string  `json:"release_date"`
	KnowledgeCutoff          *string  `json:"knowledge_cutoff"`
	OpenWeights              *int32   `json:"open_weights"`
	ModelType                *string  `json:"model_type"`
	UpstreamStatus           *string  `json:"upstream_status"`
	SupportsToolCall         *int32   `json:"supports_tool_call"`
	SupportsReasoning        *int32   `json:"supports_reasoning"`
	SupportsStructuredOutput *int32   `json:"supports_structured_output"`
	SupportsAttachment       *int32   `json:"supports_attachment"`
	RawJSON                  string   `json:"raw_json"`
}

// catalogKey 是「这次上游有哪些条目」的清单，只带主键那两列。
//
// 删下架条目只需要认人，不需要内容：整包 6 MB，为了删几行再传一遍完整 payload
// 不值。实测 9000 行时这条反连接 25 ms，而整包反序列化再比一遍要 200 ms 量级。
type catalogKey struct {
	ProviderID string `json:"provider_id"`
	ExternalID string `json:"external_id"`
}

// catalogPayload 把条目摊成两段 JSON：一段给 upsert，一段给「删掉不在其中的」。
//
// 两段都从同一个 entries 生成，所以「写进去的」与「留下来的」天然是同一批，
// 不会出现「删的时候漏了一条刚写的」。
func catalogPayload(entries []catalog.Entry) (payload, keys []byte, err error) {
	rows := make([]upsertRow, 0, len(entries))
	keyList := make([]catalogKey, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, upsertRow{
			ProviderID:               e.ProviderID,
			ProviderName:             e.ProviderName,
			ExternalID:               e.ExternalID,
			Name:                     e.Name,
			CanonicalID:              optText(e.CanonicalID),
			VendorSlug:               optText(e.VendorSlug),
			Family:                   optText(e.Family),
			Description:              e.Description,
			ModalitiesIn:             e.ModalitiesIn,
			ModalitiesOut:            e.ModalitiesOut,
			ContextWindow:            e.ContextWindow,
			MaxOutput:                e.MaxOutput,
			CostIn:                   e.CostIn,
			CostOut:                  e.CostOut,
			ReleaseDate:              optText(e.ReleaseDate),
			KnowledgeCutoff:          optText(e.Knowledge),
			OpenWeights:              e.OpenWeights,
			ModelType:                optText(e.ModelType),
			UpstreamStatus:           optText(e.UpstreamState),
			SupportsToolCall:         e.SupportsToolCall,
			SupportsReasoning:        e.SupportsReasoning,
			SupportsStructuredOutput: e.SupportsStructuredOutput,
			SupportsAttachment:       e.SupportsAttachment,
			RawJSON:                  e.RawJSON,
		})
		keyList = append(keyList, catalogKey{ProviderID: e.ProviderID, ExternalID: e.ExternalID})
	}
	if payload, err = json.Marshal(rows); err != nil {
		return nil, nil, fmt.Errorf("序列化目录条目失败: %w", err)
	}
	if keys, err = json.Marshal(keyList); err != nil {
		return nil, nil, fmt.Errorf("序列化目录键失败: %w", err)
	}
	return payload, keys, nil
}

// optText 把空串变成 JSON null。可空列走它，理由见 upsertRow。
func optText(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
