package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 未分类的错误必须留痕。
//
// 这条测试是补出来的：`ck_demos_time_order` 那次偶发 503 排查了很久，因为
// writeReviewError 的 default 分支只回「服务暂时不可用」、一行日志都不写——
// 线上看到的是 503，日志里是空的。把「有没有这条日志」也钉住，下次再出现
// 同类问题至少知道去哪看。
func TestWriteReviewErrorLogsUnclassified(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	rec := httptest.NewRecorder()
	newTestHandler(nil, nil, nil).writeReviewError(rec, errors.New("boom"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未分类错误应当回 503，实际 %d", rec.Code)
	}
	if got := decode(t, rec.Body.Bytes())["code"]; got != "unavailable" {
		t.Fatalf("错误码 = %v，想要 unavailable", got)
	}
	if !strings.Contains(buf.String(), "审核链路未分类错误") {
		t.Fatalf("未分类错误没写日志，日志内容：%q", buf.String())
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("日志里没有原始错误，排查时还是看不出是哪一层：%q", buf.String())
	}
}
