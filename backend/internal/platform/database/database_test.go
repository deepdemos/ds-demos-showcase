package database

import (
	"context"
	"testing"
)

func TestOpenRejectsEmptyURL(t *testing.T) {
	if _, err := Open(context.Background(), ""); err == nil {
		t.Fatal("空连接串应当报错")
	}
}

func TestOpenRejectsMalformedURL(t *testing.T) {
	// pgxpool.ParseConfig 在建立连接池之前就会解析连接串，格式错必须立刻失败——
	// 不能拖到第一次请求才发现。
	if _, err := Open(context.Background(), "definitely-not-a-dsn"); err == nil {
		t.Fatal("非法连接串应当在启动期就报错")
	}
}

func TestOpenDoesNotConnectEagerly(t *testing.T) {
	// 指向一个必定没有人监听的端口：Open 只建池不连库，所以这里不应该报错。
	// 这是有意的——数据库晚启动或临时不可用时，API 仍然要能起来，由 /healthz 报 503。
	pool, err := Open(context.Background(), "postgres://user:pass@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("Open 不应立刻连库: %v", err)
	}
	pool.Close()
}
