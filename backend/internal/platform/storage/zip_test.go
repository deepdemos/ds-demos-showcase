package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name string
	body string
	mode os.FileMode
}

func buildZip(t *testing.T, entries ...entry) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatalf("创建成员 %q: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("写入成员 %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip: %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("打开 zip: %v", err)
	}
	return r
}

func extractTo(t *testing.T, zr *zip.Reader, lim Limits) (string, ExtractStats, error) {
	t.Helper()
	// dst 放在 t.TempDir() 下、且**本身还不存在**——ExtractZip 要求目标是全新目录。
	dst := filepath.Join(t.TempDir(), "out")
	stats, err := ExtractZip(context.Background(), zr, dst, lim)
	return dst, stats, err
}

func TestExtractZipHappyPath(t *testing.T) {
	zr := buildZip(t,
		entry{name: "index.html", body: "<html>ok</html>"},
		entry{name: "assets/app.js", body: "console.log(1)"},
	)
	dst, stats, err := extractTo(t, zr, DefaultLimits())
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if stats.Files != 2 {
		t.Fatalf("Files = %d, want 2", stats.Files)
	}
	got, err := os.ReadFile(filepath.Join(dst, "assets", "app.js"))
	if err != nil {
		t.Fatalf("读取解出的文件: %v", err)
	}
	if string(got) != "console.log(1)" {
		t.Fatalf("内容 = %q", got)
	}
}

// 第一道闸：`..` 一律拒（不做归一化，因为归一化会把 a/../b 悄悄变成 b）。
func TestExtractZipRejectsDotDot(t *testing.T) {
	zr := buildZip(t, entry{name: "a/../../etc/passwd", body: "x"})
	dst, _, err := extractTo(t, zr, DefaultLimits())
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("应返回 ErrUnsafePath，实际 %v", err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("失败后目标目录必须被清掉，实际 stat=%v", statErr)
	}
}

func TestExtractZipRejectsAbsolutePath(t *testing.T) {
	zr := buildZip(t, entry{name: "/etc/passwd", body: "x"})
	if _, _, err := extractTo(t, zr, DefaultLimits()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("应返回 ErrUnsafePath，实际 %v", err)
	}
}

// 软链直接拒，不跳过——一个静态资源包里出现软链本身就反常。
func TestExtractZipRejectsSymlink(t *testing.T) {
	zr := buildZip(t, entry{name: "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777})
	if _, _, err := extractTo(t, zr, DefaultLimits()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("应返回 ErrUnsafePath，实际 %v", err)
	}
}

// 隐藏文件跳过，不算成员。
func TestExtractZipSkipsHiddenFiles(t *testing.T) {
	zr := buildZip(t,
		entry{name: "index.html", body: "ok"},
		entry{name: ".env", body: "SECRET=1"},
		entry{name: ".git/config", body: "x"},
	)
	dst, stats, err := extractTo(t, zr, DefaultLimits())
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if stats.Files != 1 {
		t.Fatalf("Files = %d, want 1（隐藏文件应被跳过）", stats.Files)
	}
	if _, err := os.Stat(filepath.Join(dst, ".env")); !os.IsNotExist(err) {
		t.Fatal(".env 不应被解出")
	}
}

func TestExtractZipTooManyMembers(t *testing.T) {
	zr := buildZip(t,
		entry{name: "a.txt", body: "a"},
		entry{name: "b.txt", body: "b"},
	)
	lim := DefaultLimits()
	lim.MaxMembers = 1
	if _, _, err := extractTo(t, zr, lim); !errors.Is(err, ErrTooManyMembers) {
		t.Fatalf("应返回 ErrTooManyMembers，实际 %v", err)
	}
}

func TestExtractZipTooLarge(t *testing.T) {
	zr := buildZip(t, entry{name: "big.txt", body: strings.Repeat("a", 100)})
	lim := DefaultLimits()
	lim.MaxUncompressed = 10
	if _, _, err := extractTo(t, zr, lim); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("应返回 ErrTooLarge，实际 %v", err)
	}
}

// 压缩比口径必须是「累计 ÷ 累计」，且只在累计解压量达到门槛时才判。
// 旧栈写成「累计 ÷ 当前成员压缩量」，多文件包只要最后一个是小文件就会误伤正常作品。
func TestExtractZipRatioGate(t *testing.T) {
	big := strings.Repeat("a", 1<<20) // 1 MiB 的重复内容，压缩后极小
	zr := buildZip(t, entry{name: "bomb.txt", body: big})
	lim := DefaultLimits()
	lim.MaxUncompressed = 16 << 20
	lim.RatioMinBytes = 1024
	lim.MaxRatio = 10
	if _, _, err := extractTo(t, zr, lim); !errors.Is(err, ErrSuspiciousRatio) {
		t.Fatalf("应返回 ErrSuspiciousRatio，实际 %v", err)
	}
}

// 小包不判压缩比：总量低于门槛时压缩比再高也不构成磁盘威胁。
func TestExtractZipRatioIgnoredBelowThreshold(t *testing.T) {
	zr := buildZip(t, entry{name: "small.txt", body: strings.Repeat("a", 256)})
	lim := DefaultLimits()
	lim.RatioMinBytes = 1 << 20 // 门槛远高于包内总量
	lim.MaxRatio = 2
	if _, _, err := extractTo(t, zr, lim); err != nil {
		t.Fatalf("低于门槛不该判压缩比，实际 %v", err)
	}
}

// noise 生成近似随机的字节，用来模拟「正常作品包」：真实资源（图片、压缩过的 js）
// 压缩率接近 1:1，不会触发压缩比闸。
func noise(n int) string {
	b := make([]byte, n)
	x := uint32(12345)
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 16)
	}
	return string(b)
}

// 多文件时「累计 ÷ 累计」不该被最后一个小文件带偏：
// 前面几个大文件正常压缩，最后一个只有几十字节，整体比值仍然合理。
func TestExtractZipRatioMultiFileNotHijackedBySmallTail(t *testing.T) {
	zr := buildZip(t,
		entry{name: "a.bin", body: noise(400 << 10)},
		entry{name: "b.bin", body: noise(400 << 10)},
		entry{name: "c.txt", body: "z"}, // 尾巴只有 1 字节
	)
	lim := DefaultLimits()
	lim.RatioMinBytes = 64 << 10
	lim.MaxRatio = 100
	if _, _, err := extractTo(t, zr, lim); err != nil {
		t.Fatalf("正常多文件包不该被判成炸弹，实际 %v", err)
	}
}

func TestExtractZipEmptyArchive(t *testing.T) {
	zr := buildZip(t, entry{name: "dir/", body: ""})
	if _, _, err := extractTo(t, zr, DefaultLimits()); !errors.Is(err, ErrEmptyArchive) {
		t.Fatalf("应返回 ErrEmptyArchive，实际 %v", err)
	}
}

func TestExtractZipRefusesExistingTarget(t *testing.T) {
	zr := buildZip(t, entry{name: "index.html", body: "ok"})
	dst := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("准备目录: %v", err)
	}
	if _, err := ExtractZip(context.Background(), zr, dst, DefaultLimits()); err == nil {
		t.Fatal("目标已存在时应报错")
	}
}

func TestExtractZipRejectsZeroLimits(t *testing.T) {
	zr := buildZip(t, entry{name: "index.html", body: "ok"})
	if _, _, err := extractTo(t, zr, Limits{}); err == nil {
		t.Fatal("上限未配置时应报错")
	}
}

func TestCheckRatio(t *testing.T) {
	lim := DefaultLimits()
	if err := CheckRatio(ExtractStats{Uncompressed: 1 << 20, Compressed: 1 << 20}, lim); err != nil {
		t.Fatalf("1:1 不该报错: %v", err)
	}
	if err := CheckRatio(ExtractStats{Uncompressed: 100 << 20, Compressed: 100 << 10}, lim); !errors.Is(err, ErrSuspiciousRatio) {
		t.Fatalf("1000:1 应报错，实际 %v", err)
	}
	// 压缩量为 0（全是未压缩存储的成员）时不判——没有分母。
	if err := CheckRatio(ExtractStats{Uncompressed: 100 << 20}, lim); err != nil {
		t.Fatalf("压缩量为 0 时不该报错: %v", err)
	}
}
