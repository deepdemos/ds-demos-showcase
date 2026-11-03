// Package storage 负责作品文件的对象存储与安全解压。
//
// 它属于 platform 层：**只认 key，不认 slug 是否合法**。slug 的校验、生成、冲突处理
// 都是 Demo 域的事（`internal/domain/demo`），这里只把它们当成路径片段用。
//
// 为什么先定接口：本期只有本地磁盘，但 key 约定与接口形状按「将来换 OSS 不用改调用方」
// 设计。旧栈的教训是 OSS 上传失败要能降级到本地而不影响提交（`legacy-main` 的
// `_oss_upload_safe` 就是这个意思），所以调用方拿到的永远是「本地已经可用」的语义。
package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound 表示 key 不存在。调用方用它区分「没这个文件」与「存储坏了」。
var ErrNotFound = errors.New("storage: 对象不存在")

// ErrUnsafeKey 表示 key 试图逃出存储根目录（`..`、绝对路径、盘符）。
//
// key 由本仓库代码生成，正常情况下不会命中；但存储层不做「上游一定是对的」的假设——
// 一旦某天有人把用户输入拼进 key，这里必须是最后一道闸。
var ErrUnsafeKey = errors.New("storage: 非法的对象 key")

// Store 是对象存储的抽象。
//
// 语义约定（两个实现都必须满足）：
//   - Put 是原子的：要么新内容完整可见，要么旧内容原样保留。半个文件不允许被观察到。
//   - Get 返回的 ReadCloser 由调用方关闭；关闭后不得再读。
//   - Delete 删除不存在的 key 不算错误（幂等）。
//   - URL 只有「对象可被浏览器直接访问」时才返回 true——本地磁盘没有这种 URL。
type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (size int64, err error)
	URL(key string) (string, bool)
}

// FilesPrefix 返回某作品**某个版本**解压后文件目录的 key 前缀。
//
// 为什么按版本分目录而不是 `demos/{slug}/files/`：版本是不可变的（DEMO-014「一旦创建
// 不可修改」）。分版本之后「更新作品」就永远不会覆盖旧文件——旧版本要么还在被引用，
// 要么等着被 GC，但绝不会出现「读到半新半旧的目录」。
//
// 这也省掉了旧栈的 `_atomic_replace_dir`：没有覆盖，就不需要原子替换。
// 前缀仍以旧栈的 `demos/{slug}/` 开头，迁 OSS 时历史对象能直接对上。
func FilesPrefix(slug string, revNo int) string {
	return "demos/" + slug + "/r" + itoa(revNo) + "/"
}

// FilesKey 返回作品内某个相对路径对应的 key（rel 用 `/` 分隔）。
func FilesKey(slug string, revNo int, rel string) string {
	return FilesPrefix(slug, revNo) + rel
}

// SourceKey 返回原始上传包（zip / 单文件）的 key。
//
// 保留原件是为了两件事：重新解压（解压逻辑修好后能就地修旧作品）、以及「下载原件」
// 这个用户动作。它也按版本分，理由同上。
func SourceKey(slug string, revNo int, ext string) string {
	return "demos/" + slug + "/r" + itoa(revNo) + "/source." + ext
}

// CoverKey 返回封面（按版本）的 key。
func CoverKey(slug string, revNo int, ext string) string {
	return "demos/" + slug + "/r" + itoa(revNo) + "/cover." + ext
}

// itoa 避免为了拼 key 引入 strconv（这个文件只做字符串拼接，保持零依赖）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
