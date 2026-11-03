package demo

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// canonicalManifest 按设计文档（Demo 域 §3.2）算清单哈希：
//
//	canonical = 按 path 升序的 "path" + NUL + blob_sha256 + NL 拼接
//	manifest_hash = sha256(canonical)
//
// 为什么不用整个 zip 的 sha256：那样**同样的内容换个打包工具、换台机器的 mtime，
// 哈希就变了**，而设计明确要求「与 zip 如何打包无关」。zip 字节哈希只是
// 「这个包文件本身」的指纹，与「作品内容是什么」不是一回事。
func canonicalManifest(baseDir string, rels []string) (string, error) {
	sorted := make([]string, len(rels))
	copy(sorted, rels)
	sort.Strings(sorted)

	h := sha256.New()
	for _, rel := range sorted {
		sum, err := hashFile(filepath.Join(baseDir, rel))
		if err != nil {
			return "", err
		}
		// 用相对路径（正斜杠）而不是绝对路径：换台机器解压目录不同，但内容相同。
		_, _ = io.WriteString(h, filepath.ToSlash(rel))
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, sum)
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
