// Package identity 是身份域的业务逻辑。第一条竖切只做「注册 + 我是谁」。
//
// 它不认识 HTTP：不 import net/http，也不 import 生成的 api 包。
// 这一条由 check-layering 的 L2 强制——业务规则不该被传输层的形状绑架。
package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2 参数来自身份域 §11.4，不是随手定的默认值：
// m=64MiB 让一次哈希的内存成本高到难以并行穷举，t=3 在交互可接受的前提下加时间成本。
// 改这些数等于改安全下限——它们登记在 docs/运行手册-可变常量.md §3。
const (
	argonMemoryKiB  = 64 * 1024
	argonIterations = 3
	argonThreads    = 2
	argonKeyLen     = 32
	argonSaltLen    = 16
)

// ErrBadHash 表示存进去的那串不是合法的 PHC 编码——属于数据损坏，不是用户输错。
var ErrBadHash = errors.New("identity: 密码哈希格式不合法")

// HashPassword 产出 PHC 编码串：$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>。
//
// 为什么自己拼而不是再引一个库：PHC string format 是公开标准，拼出来任何语言都能验；
// 为这一个函数再加一个密码库不划算。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: 生成盐失败: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonThreads, argonKeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 用**哈希串里记录的参数**验密码，而不是用包级常量。
//
// 这一点很关键：将来调高 m/t/p 时，老用户的哈希仍按老参数验证，不需要重置所有人的密码。
// 反过来用当前常量去验老哈希的话，调参那天所有老账号会同时登不上。
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrBadHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrBadHash
	}

	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, ErrBadHash
	}
	// 参数为 0 会让 argon2 立刻返回一串可预测的字节——必须挡住，否则一条损坏（或伪造）
	// 的哈希行就变成「任何密码都能过」。
	if memory == 0 || iterations == 0 || threads == 0 {
		return false, ErrBadHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrBadHash
	}

	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	// 定长比较：普通 == 在第一个不同字节就返回，泄漏「错了几个字节」。
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// 26 个字符、全大写、无歧义字符——抄写或口述不容易错。
var subEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSub 生成对外不可变 ID：base32(128 bit)，26 个字符（标准对象 §ID-3）。
//
// 不暴露自增主键；不用 UUID 是因为 base32 更短且无歧义字符。
func NewSub() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("identity: 生成 sub 失败: %w", err)
	}
	return subEncoding.EncodeToString(raw), nil
}

// newSessionID 生成会话 ID：32 字节随机数、base64url 无填充、43 个字符。
// 它是浏览器端的不透明串——不进日志，也不出现在 URL 里。
func newSessionID() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("identity: 生成会话 ID 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
