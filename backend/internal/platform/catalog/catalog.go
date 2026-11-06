// Package catalog 把 models.dev 的整包 JSON 解析成可以直接落库的条目。
//
// 它是 platform 层：不知道数据库，也不知道 HTTP 契约。调用方给字节，它给条目。
// 这样测试可以喂 fixture——上游整包 5.96 MB 走外网，CI 里断网就红，
// 而且是那种「昨天还好好的」红。
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Source 是这一份目录的来源标识，写进 `model_catalog_items.source`。
const Source = "models.dev"

// Entry 是一条可以直接落库的目录条目（对应 `model_catalog_items` 的一行）。
type Entry struct {
	ProviderID               string
	ProviderName             string
	ExternalID               string
	Name                     string
	CanonicalID              string
	VendorSlug               string
	Family                   string
	Description              string
	ModalitiesIn             string
	ModalitiesOut            string
	ContextWindow            *int32
	MaxOutput                *int32
	CostIn                   *float64
	CostOut                  *float64
	ReleaseDate              string
	Knowledge                string
	OpenWeights              *int32
	ModelType                string
	UpstreamState            string
	SupportsToolCall         *int32
	SupportsReasoning        *int32
	SupportsStructuredOutput *int32
	SupportsAttachment       *int32
	RawJSON                  string
}

// provider 是上游 JSON 的一个厂商节点。
type provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Models map[string]model `json:"models"`
}

// model 是上游 JSON 的一个模型节点。
//
// 只声明用得上的字段：整包里有 20 多种字段，多声明一个就多一处「上游改名后静默变空」。
// 原始片段整段留在 `RawJSON` 里，将来要用别的字段不必重新拉包。
type model struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Family      string `json:"family"`
	CanonicalID string `json:"canonical_model_id"`
	ModelType   string `json:"type"`
	Status      string `json:"status"`
	Knowledge   string `json:"knowledge"`
	ReleaseDate string `json:"release_date"`
	OpenWeights *bool  `json:"open_weights"`
	ToolCall    *bool  `json:"tool_call"`
	Reasoning   *bool  `json:"reasoning"`
	Structured  *bool  `json:"structured_output"`
	Attachment  *bool  `json:"attachment"`
	Limit       struct {
		Context *int32 `json:"context"`
		Output  *int32 `json:"output"`
	} `json:"limit"`
	Cost struct {
		Input  *float64 `json:"input"`
		Output *float64 `json:"output"`
	} `json:"cost"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
}

// Parse 把整包字节解析成条目，按 provider 排序保证结果稳定（同步是幂等的，
// 同一份字节两次解析必须给出同样的行序，否则 diff 测试与人工核对都没有基准）。
func Parse(raw []byte) ([]Entry, error) {
	var providers map[string]provider
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("解析 models.dev 目录失败: %w", err)
	}
	if len(providers) == 0 {
		// 空对象是合法的 JSON，但作为目录它意味着「上游没给数据」。当成错误：
		// 一次「成功」的空同步会把现有镜像清空，然后运行时看起来一切正常。
		return nil, fmt.Errorf("目录为空: 上游返回了 0 个厂商")
	}

	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Entry, 0, 1024)
	for _, pid := range ids {
		p := providers[pid]
		if p.ID == "" {
			p.ID = pid
		}
		modelIDs := make([]string, 0, len(p.Models))
		for mid := range p.Models {
			modelIDs = append(modelIDs, mid)
		}
		sort.Strings(modelIDs)

		for _, mid := range modelIDs {
			m := p.Models[mid]
			externalID := m.ID
			if externalID == "" {
				externalID = mid
			}
			raw, err := json.Marshal(m)
			if err != nil {
				return nil, fmt.Errorf("序列化 %s/%s 失败: %w", p.ID, externalID, err)
			}
			e := Entry{
				ProviderID:               p.ID,
				ProviderName:             p.Name,
				ExternalID:               externalID,
				Name:                     Name(m.Name, externalID),
				CanonicalID:              m.CanonicalID,
				Family:                   m.Family,
				Description:              m.Description,
				ModalitiesIn:             strings.Join(m.Modalities.Input, ","),
				ModalitiesOut:            strings.Join(m.Modalities.Output, ","),
				ContextWindow:            m.Limit.Context,
				MaxOutput:                m.Limit.Output,
				CostIn:                   m.Cost.Input,
				CostOut:                  m.Cost.Output,
				ReleaseDate:              Date(m.ReleaseDate),
				Knowledge:                m.Knowledge,
				OpenWeights:              boolInt(m.OpenWeights),
				ModelType:                m.ModelType,
				UpstreamState:            m.Status,
				SupportsToolCall:         boolInt(m.ToolCall),
				SupportsReasoning:        boolInt(m.Reasoning),
				SupportsStructuredOutput: boolInt(m.Structured),
				SupportsAttachment:       boolInt(m.Attachment),
				RawJSON:                  string(raw),
			}
			if m.CanonicalID != "" {
				e.VendorSlug = VendorSlug(m.CanonicalID)
			}
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("目录为空: 上游返回了 0 条模型")
	}
	return out, nil
}

// Name 取展示名。名字缺失时退回外部 id——空名字在界面上是一行看不见的选项。
func Name(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return fallback
}

// VendorSlug 取 canonical 的厂商前缀（`deepseek/deepseek-v4-flash` → `deepseek`）。
//
// 没有前缀时返回空串而不是编一个：编出来的厂商会把「同一厂商的模型」这条判断
// 变成假的，而 `vendor_slug` 正是用来做这条判断的。
func VendorSlug(canonical string) string {
	i := strings.Index(canonical, "/")
	if i <= 0 {
		return ""
	}
	return canonical[:i]
}

// Date 校验并规整上游日期。
//
// 上游是 `YYYY-MM-DD`；不是这个形状就返回空串（落库为 NULL），而不是硬塞进去让
// PostgreSQL 报错——一条坏日期不该让整次同步失败，9000 条里有一条畸形是常态。
//
// 用 time.Parse 而不是检查字符形状：`2026-10-99` 十一位数字全对，但不是一天。
// 形状检查会把这种值放行，然后在 INSERT 时炸掉整次同步（连带回滚 8999 条好数据）。
func Date(raw string) string {
	raw = strings.TrimSpace(raw)
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func boolInt(b *bool) *int32 {
	if b == nil {
		return nil
	}
	var v int32
	if *b {
		v = 1
	}
	return &v
}

// DefaultURL 是上游整包的地址。
const DefaultURL = "https://models.dev/api.json"

// maxPayload 是抓取上限（64 MiB）。当前整包约 6 MB，留 10 倍余量；
// 没有上限的话，一个被劫持的域名可以让我们读到磁盘满。
const maxPayload = 64 << 20

// Fetcher 拉上游整包。它只做「拿到字节」，解析交给 Parse。
//
// 分开是为了测试：解析有 fixture 就够，抓取逻辑用 httptest 就能覆盖，
// 两边都不必真的联网。
type Fetcher struct {
	URL    string
	Client *http.Client
}

// NewFetcher 造一个带超时的 Fetcher。
//
// 超时给 60 秒：5.96 MB 在慢链路上也可能要十几秒，而更长的等待只会让
// 「上游挂了」这件事更晚被发现。
func NewFetcher(url string, client *http.Client) *Fetcher {
	if url == "" {
		url = DefaultURL
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Fetcher{URL: url, Client: client}
}

// Fetch 取回整包字节。
func (f *Fetcher) Fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造目录请求失败: %w", err)
	}
	// 带 UA：上游是个公共服务，出事时要能从日志里认出是谁。
	req.Header.Set("User-Agent", "ds-demos-showcase/1.0 (model catalog sync)")
	req.Header.Set("Accept", "application/json")

	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取目录失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("拉取目录失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPayload+1))
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}
	if len(body) > maxPayload {
		return nil, fmt.Errorf("目录超过 %d MiB，拒绝解析", maxPayload>>20)
	}
	return body, nil
}

// Hash 算 payload 指纹，写进 `model_source_sync.payload_hash`。
//
// 用途是回答「上游到底变没变」：两次抓到的指纹相同，就不必重新解析、重写 9000 行。
func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
