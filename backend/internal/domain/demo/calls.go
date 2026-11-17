package demo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"
)

// ModelCall 是一条「端点上报」：调用方（插件、脚本）说「我在这个地址上用的是这个模型名」。
//
// 它是 #445 的 4d：`model_offerings` 有两个出处，目录那一路（authority='models.dev'）由
// 导入写，端点这一路（authority='endpoint'）此前没有任何写入方。上报并进投稿 / 编辑表单
// 而不是单开一个端点，理由见 #404 与 issue #445：它天然带作者与作品上下文，也不用先替
// #408（长期可撤销凭据）拍板。
//
// 三个字段都按「调用方原样说」收，规范化与校验在 prepareModelCalls 里做：
//   - Ref 是上报的模型名，与 modelSlugs 一样按指称解析（slug / 展示名 / 别名）。
//   - Endpoint 是 base_url，落库前规范化（小写、去默认端口、去尾斜杠、丢查询与片段）。
//   - Checkpoint 是版本 label，可空；空了就是「不知道哪一版」，落 pinning='unknown'。
type ModelCall struct {
	Ref        string `json:"ref"`
	Endpoint   string `json:"endpoint"`
	Checkpoint string `json:"checkpoint"`
}

const (
	// maxModelCalls 是一次投稿 / 编辑最多带几条端点上报。
	//
	// 与 modelSlugs 不同，这一项每条都要解析、可能要建版本行、还要写一行调用，
	// 成本随条数线性涨。50 的依据：一件作品声明的模型通常 1~3 个，极端的多模型
	// 编排作品在旧站实测最多 11 个；50 留足余量又不至于让一次请求变成批量导入。
	maxModelCalls = 50

	// maxEndpointLen 与 `ck_model_offerings_provider_id` 的 0..512 一致。
	//
	// 库层那条 CHECK 是最后一道，这里先挡成 400：超长输入不该以 23514 的形式
	// 变成 500。两个数字必须一起改（`docs/运行手册-可变常量.md` 有登记）。
	maxEndpointLen = 512

	// maxCheckpointLen 与 `ck_model_checkpoints_label` 的 1..64 一致。
	maxCheckpointLen = 64

	// maxCallJSONLen 是单条上报（JSON 文本）的长度上限，与契约里的 maxLength 一致。
	//
	// 三个字段的上限相加（512 + 128 + 64）再加键名与引号，远小于这个数；设它是为了
	// 在解码前就把「一条上报里塞了一兆」挡成 400，而不是先解析再逐字段报错。
	maxCallJSONLen = 2048

	// pinningExact / pinningUnknown 是 `model_offerings.pinning` 的两个取值。
	//
	// 上报带了版本就是 exact（「我调的就是这一版」）；没带就是 unknown——
	// 不知道就写不知道，不假装知道（#404 取舍 4）。`rolling` 留给将来的
	// 「这个别名永远指向当前版」的判定，本刀没有写入方。
	pinningExact   = "exact"
	pinningUnknown = "unknown"
)

// parseModelCalls 把 multipart 里重复出现的 `modelCalls` 字段解成结构。
//
// 为什么每一项是一个 JSON 字符串而不是嵌套对象：multipart 的字段值只能是字符串
// （浏览器的 FormData 也只能 append 字符串或 Blob），嵌套对象没有标准编码。
// 按项 JSON 编码是唯一不需要发明新语法、又能让每一项独立报错的做法。
//
// 不认识的键直接 400（`DisallowUnknownFields`）：把 `endpont` 这种拼错静默丢掉，
// 结果是「我明明上报了端点，库里却是 unknown」——比报错难查得多。
//
// 报错一律不回显原文：这一条里可能就带着凭据，而 400 的正文会进客户端日志、
// 浏览器提示、用户贴进 issue 的截图——D03 的立意是凭据不进任何地方。
func parseModelCalls(raw []string) ([]ModelCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxModelCalls {
		return nil, fmt.Errorf("%w: 端点上报最多 %d 条，这次给了 %d 条", ErrInvalidInput, maxModelCalls, len(raw))
	}
	out := make([]ModelCall, 0, len(raw))
	for i, s := range raw {
		if n := utf8.RuneCountInString(s); n > maxCallJSONLen {
			return nil, fmt.Errorf("%w: 单条端点上报最长 %d 个字符，这条有 %d 个", ErrInvalidInput, maxCallJSONLen, n)
		}
		var c ModelCall
		dec := json.NewDecoder(strings.NewReader(s))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("%w: 第 %d 条端点上报不是一个合法的 JSON 对象：%v", ErrInvalidInput, i+1, err)
		}
		// 一个字段里塞两个对象时，标准库只解第一个、剩下的静默丢掉——与「拼错的键要
		// 报错」同一个理由：宁可 400，也不要让一半数据消失。
		if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
			return nil, fmt.Errorf("%w: 第 %d 条端点上报里只能有一个 JSON 对象（后面还有内容）", ErrInvalidInput, i+1)
		}
		out = append(out, c)
	}
	return out, nil
}

// preparedCall 是校验与规范化之后的调用上报，字段与落库列一一对应。
type preparedCall struct {
	Ref          string // 上报的模型名，落 external_id
	Endpoint     string // 规范化后的 base_url，落 provider_id；空串表示「只报版本，不报端点」
	ProviderName string // 给人看的 host，落 provider_name
	Checkpoint   string // 版本 label，可空
}

// prepareModelCalls 校验并规范化上报，按 (端点, 名字) 去重。
//
// 去重规则：同一个 (endpoint, ref) 出现多次时**后一次覆盖前一次**，位置留在第一次出现处。
// 这与 `UpsertEndpointOffering` 的「后一次覆盖前一次」一致——库里只有一行，报两次
// 不同的版本时以最后一次为准，不会出现「请求里说 A、库里是 B」的错位。
func prepareModelCalls(calls []ModelCall) ([]preparedCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]preparedCall, 0, len(calls))
	index := make(map[string]int, len(calls))
	for _, c := range calls {
		ref := strings.TrimSpace(c.Ref)
		if ref == "" {
			// D06：空名字或纯空白一律拒收——它连「指的是谁」都说不出来。
			return nil, fmt.Errorf("%w: 端点上报缺模型名（ref）", ErrInvalidInput)
		}
		if len([]rune(ref)) > maxModelNameLen {
			return nil, fmt.Errorf("%w: 端点上报的模型名最长 %d 个字：%s", ErrInvalidInput, maxModelNameLen, abbreviate(ref))
		}
		cp := strings.TrimSpace(c.Checkpoint)
		if len([]rune(cp)) > maxCheckpointLen {
			return nil, fmt.Errorf("%w: 版本号最长 %d 个字：%s", ErrInvalidInput, maxCheckpointLen, abbreviate(cp))
		}
		pc := preparedCall{Ref: ref, Checkpoint: cp}
		if raw := strings.TrimSpace(c.Endpoint); raw != "" {
			normalized, err := normalizeEndpoint(raw)
			if err != nil {
				return nil, err
			}
			pc.Endpoint = normalized
			pc.ProviderName = hostOf(normalized)
		}
		key := pc.Endpoint + "\x00" + strings.ToLower(pc.Ref)
		if i, dup := index[key]; dup {
			out[i] = pc
			continue
		}
		index[key] = len(out)
		out = append(out, pc)
	}
	return out, nil
}

// normalizeEndpoint 把 base_url 规范化成「同一个出处只有一种写法」（D02）。
//
// 规则：
//   - scheme 与 host 小写；只收 http / https（别的 scheme 连「端点」都算不上）。
//   - 去掉默认端口（http 的 80、https 的 443）：`https://a.com:443/v1` 与
//     `https://a.com/v1` 是同一个出处，不去掉就会在库里变成两行。
//   - 去掉尾部斜杠：`https://a.com/v1/` 与 `https://a.com/v1` 同理。
//   - 丢掉查询串与片段：它们不改变「是哪台机器上的哪个接口」。
//   - 路径用 EscapedPath 而不是 Path：Path 已经把 `%2F` 解成 `/`，会把两个不同的
//     端点并成一行，而 `ON CONFLICT DO UPDATE` 会静默改掉前一行的归属。
//   - IPv6 字面量保留方括号：`[::1]` 去掉括号就成了 `::1`，拼出来的地址解析不回来。
//   - 带 userinfo（形如 https://用户名:口令@主机）一律 400（D03）：那是凭据，绝不能落库。
//   - 查询串里出现 token / key / secret 之类参数名也一律 400（D03）。判据是参数名，
//     不是值——值长什么样没有可靠特征，而把 `?api_key=...` 静默丢掉又等于假装没收到。
//
// 报错不回显原文：地址里可能带着凭据，而 400 的正文会进客户端日志与用户截图。
func normalizeEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse 的报错里带着整串地址，不能原样往上抛。
		return "", fmt.Errorf("%w: 端点地址看不懂（不是合法的 URL）", ErrInvalidInput)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: 端点地址要以 http:// 或 https:// 开头（收到的是 %q）",
			ErrInvalidInput, abbreviate(scheme))
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: 端点地址里不能带用户名或密码（形如 https://用户名:口令@主机 的一律拒收）", ErrInvalidInput)
	}
	for name := range u.Query() {
		if isCredentialParam(name) {
			return "", fmt.Errorf("%w: 端点地址的查询串里不能带凭据（%s）", ErrInvalidInput, name)
		}
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("%w: 端点地址缺主机名", ErrInvalidInput)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" && !isDefaultPort(scheme, port) {
		host += ":" + port
	}
	out := scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/")
	if n := utf8.RuneCountInString(out); n > maxEndpointLen {
		return "", fmt.Errorf("%w: 端点地址最长 %d 个字符，这条有 %d 个", ErrInvalidInput, maxEndpointLen, n)
	}
	return out, nil
}

// isDefaultPort 判断端口是不是这个 scheme 的默认端口。
func isDefaultPort(scheme, port string) bool {
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}

// credentialParams 是「查询串里出现就拒收」的参数名（小写、去掉 - 与 _ 之后比较）。
//
// 不追求穷举：漏掉的参数名最坏结果是它被丢掉（查询串本来就不落库），
// 而错杀一个普通参数会让正常上报 400——所以这张表只收明确的凭据名。
// 唯一的例外是 key：它确实可能是个普通参数（`?key=release`），但真实 API 里
// `?key=` 几乎总是 API key。错杀的代价是一句能看懂的 400，放过的代价是一条凭据
// 进了日志——这里选错杀。
var credentialParams = map[string]struct{}{
	"token":         {},
	"accesstoken":   {},
	"refreshtoken":  {},
	"apikey":        {},
	"key":           {},
	"secret":        {},
	"clientsecret":  {},
	"password":      {},
	"passwd":        {},
	"pwd":           {},
	"sig":           {},
	"signature":     {},
	"auth":          {},
	"authorization": {},
	"credential":    {},
}

// isCredentialParam 判断一个查询参数名是不是凭据类。
func isCredentialParam(name string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
	_, ok := credentialParams[normalized]
	return ok
}

// hostOf 从规范化后的地址里取 host，用作 `model_offerings.provider_name`。
//
// 存 host 而不是空串：这一列是给人看的（provider_id 那一列是给键用的完整 base_url）。
// 目前没有读路径——模型页的「来源分布」读的是 `demo_models`，不是这张表；
// 将来要展示时记得先隐去私网地址（D04）。
func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return ""
}

// abbreviate 把可能很长的输入截短，只用于错误信息。
//
// 请求体里能塞多长的端点地址，400 的正文就会跟着长多少——回显要有限度。
func abbreviate(s string) string {
	const limit = 120
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
