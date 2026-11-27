package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/slug"
)

// SlugMaxLen 是生成建议值的字符上限。
//
// 64 而不是库上限 128：给 #587 的唯一性后缀留余量（撞名后缀长成 `-2`、`-3`…），
// 也不让 URL 长到需要换行。库里的 `ck_models_slug_len` 仍是 1–128，手工改的 slug
// 由接口的长度校验管——这里管的是「生成」这一段。数在运行手册 §7.7 登记。
const SlugMaxLen = 64

// slugDigestLen 是「名字折不出可读形态」时摘要取几位十六进制。
//
// 6 位 = 24 bit，对「站内模型的规模」够用；真撞了也不丢数据——撞名后缀会把第二条
// 兜住。**必须小于 SlugMaxLen - 1**：兜底形态要留出中间那个连字符的位置，
// 否则 6 位摘要 + 「-」+ 前缀会超上限（运行手册第 55 条按此口径登记）。
const slugDigestLen = 6

// Slug 从模型名生成公开 slug 的**建议值**。
//
// slug 是公开 URL 的一部分（`/models/<slug>`）且创建后不可变，所以生成规则必须可预测：
// 同一个名字永远折出同一个值，不掺随机数也不掺时间。这里只给基座，**不做唯一性后缀**——
// 撞名怎么处理是写入方的冲突逻辑（「-2」「-3」不是这条规则的一部分）。
//
// 规则：名字折得出 ASCII 就用它；折不出（纯中文、纯标点、纯空白）就只给名字摘要前 6 位。
// 厂商有值时前缀一段厂商 slug（`deepseek-3f9a2c`）；厂商为空时连前缀也不加，
// 但**绝不以连字符开头或结尾**——空串与 `-` 开头都会让 URL 与库约束直接出问题。
func Slug(name, vendor string) string {
	if base := slug.Fold(name); base != "" {
		return trimToLimit(base)
	}
	digest := nameDigest(name)
	head := slug.Fold(vendor)
	if head == "" {
		return digest
	}
	// 摘要必须整段保留，所以前缀只能占剩下的位置。
	room := SlugMaxLen - len(digest) - 1
	if len(head) > room {
		head = strings.TrimRight(head[:room], "-")
	}
	return head + "-" + digest
}

// trimToLimit 截到上限。截断点可能正好落在分隔符上，末尾的连字符要去掉——
// 否则 URL 会长成 `/models/xxxx-`。
func trimToLimit(base string) string {
	if len(base) > SlugMaxLen {
		base = base[:SlugMaxLen]
	}
	return strings.Trim(base, "-")
}

// nameDigest 取名字的 sha256 前 6 位小写十六进制。
//
// 用摘要而不是随机后缀：同一个名字重新生成必须得到同一段，否则「这条模型是不是
// 已经建过」会退化成人工判断。摘要算的是**原始名字的 UTF-8 字节**，不先折叠也不先
// 归一化——同一输入稳定是这条规则唯一的硬要求。
func nameDigest(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])[:slugDigestLen]
}
