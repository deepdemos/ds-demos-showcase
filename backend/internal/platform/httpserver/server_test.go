package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type failingPinger struct{ msg string }

func (f failingPinger) Ping(context.Context) error { return errors.New(f.msg) }

func decodeHealthz(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("invalid json: %v (body=%q)", err, string(body))
	}
	return parsed
}

func TestHealthzWithoutDatabase(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := decodeHealthz(t, response.Body.Bytes())["status"]; got != "ok" {
		t.Fatalf("expected status ok, got %v", got)
	}
}

func TestHealthzWithDatabaseUp(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(okPinger{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
}

func TestHealthzWithDatabaseDown(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	// 故意让错误信息里带「不能泄漏」的东西：连接串、主机名。
	const secret = "postgres://user:pass@10.1.2.3:5432/deepdemos"
	NewRouter(failingPinger{msg: secret}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}

	body := response.Body.String()
	if strings.Contains(body, "10.1.2.3") || strings.Contains(body, "postgres://") {
		t.Fatalf("响应体泄漏了内部信息: %s", body)
	}

	parsed := decodeHealthz(t, response.Body.Bytes())
	if parsed["code"] != "unavailable" {
		t.Fatalf("expected code unavailable, got %v", parsed["code"])
	}
	message, _ := parsed["message"].(string)
	if message == "" {
		t.Fatal("503 必须带 message")
	}
	if strings.Contains(message, "10.1.2.3") {
		t.Fatalf("message 泄漏了内部信息: %s", message)
	}
}
