package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// listBaseline 列出冻结基线目录里的 .sql，按文件名排序。
//
// 基线文件名是历史沿用的 `NN-名称.sql`（如 10-identity.sql），不带版本号语义——
// 它整块等价于「0000 号迁移」，所以这里不解析文件名里的数字。
func listBaseline(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读基线目录失败: %w", err)
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		path := filepath.Join(dir, name)
		sum, err := fileChecksum(path)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{name: name, path: path, checksum: sum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// fileChecksum 是「已合入的迁移不许改」这条规则的执行基础。
func fileChecksum(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读 %s 失败: %w", path, err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// dirChecksum 把一个目录里所有 .sql 的 (文件名, 内容) 拼起来算一个总校验和，
// 用来登记「基线是哪一份」。
func dirChecksum(dir string) (string, error) {
	files, err := listBaseline(dir)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		body, err := os.ReadFile(f.path)
		if err != nil {
			return "", err
		}
		h.Write([]byte(f.name))
		h.Write([]byte{0})
		h.Write(body)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
