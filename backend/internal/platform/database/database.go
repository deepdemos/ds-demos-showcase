// Package database 管理 PostgreSQL 连接池的生命周期。
//
// 设计取舍（都是有意的）：
//   - **启动时不强制 Ping**：数据库可能比 API 晚起来，或临时不可用。API 应该能起来，
//     由 /healthz 如实报告 503，而不是让进程直接退出。运维看到的是「服务在、依赖挂了」，
//     这比「服务根本没起」更容易定位。
//   - **URL 格式错误则立刻失败**：配置写错属于启动期错误，不该拖到第一次请求才发现。
//   - **连接池参数写死在这里**，不进设置域：它们是性能调优，改了要重启、要重新压测，
//     属于「结构性」而不是「运营性」（见 docs/设置域设计.md §4.1 的判据）。
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 连接池参数。改这里要重启服务，并且要重新看一遍慢查询——
// 所以它们登记在 docs/运行手册-可变常量.md，不进设置域。
const (
	maxConns          = 10
	maxConnLifetime   = 30 * time.Minute
	maxConnIdleTime   = 5 * time.Minute
	healthCheckPeriod = 30 * time.Second
)

// Open 解析连接串并建立连接池。它**不会**真的连上去——第一次查询时才连。
// 这既是取舍（见包注释），也意味着调用方必须自己保证 /healthz 如实反映可达性。
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, fmt.Errorf("database: 连接串为空")
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: 解析连接串失败: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.MaxConnIdleTime = maxConnIdleTime
	cfg.HealthCheckPeriod = healthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: 建立连接池失败: %w", err)
	}
	return pool, nil
}
