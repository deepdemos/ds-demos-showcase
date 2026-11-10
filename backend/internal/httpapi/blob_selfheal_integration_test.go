package httpapi

import (
	"net/http"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newRootHandler 用指定的存储根组装一套 handler + preview。
//
// 与 newShapeHandler 的唯一区别是这个根可控：自愈用例要两套存储根对着同一个库。
func newRootHandler(t *testing.T, pool *pgxpool.Pool, idSvc *identity.Service, root string) (*Handler, *Preview) {
	t.Helper()
	q := store.New(pool)
	files, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), NewPreview(q, files, idSvc)
}

// TestIntegrationBlobRowWithoutObjectIsRewritten 钉住一条会静默失败的不变量：
//
// 「blobs 表里有这一行」不等于「存储里有这份字节」。换存储根、或者从只备份了
// 数据库的快照恢复之后，行全在、对象全没——那时若只按行判断「已经在库了」，
// 后续每一次上传都会跳过写盘，于是预览与下载永远 404，而错误是「对象不存在」。
//
// 这里用两套存储根对同一个库来复现：第一套写完，第二套再投同一份内容。
// 旧实现会认为「已经有了」而不写，第二套的存储根里什么都没有。
func TestIntegrationBlobRowWithoutObjectIsRewritten(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "自愈"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	content := []byte("<!doctype html><html><body>自愈用例 " + uniq() + "</body></html>")

	first, firstPreview := newRootHandler(t, pool, idSvc, t.TempDir())
	slugA := submitShape(t, first, sid, "自愈-一", "index.html", content)
	if rec := previewGet(firstPreview, "/preview/"+slugA+"/v1/index.html", sid); rec.Code != http.StatusOK {
		t.Fatalf("第一套存储读自己的文件 = %d，想要 200", rec.Code)
	}

	// 第二套存储根是空的，但库里已经有这份内容的 blob 行（内容相同的 sha256）。
	second, secondPreview := newRootHandler(t, pool, idSvc, t.TempDir())
	slugB := submitShape(t, second, sid, "自愈-二", "index.html", content)
	if rec := previewGet(secondPreview, "/preview/"+slugB+"/v1/index.html", sid); rec.Code != http.StatusOK {
		t.Fatalf("同一份内容投到另一套存储 = %d，想要 200（对象不存在时应当补写，而不是相信数据库那一行）", rec.Code)
	}
}
