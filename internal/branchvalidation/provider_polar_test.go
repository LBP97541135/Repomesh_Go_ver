package branchvalidation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/testdb"
)

func polarSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}

// 未配置 / 没给业务基线时**必须明确报错** —— 不假装开出了分支，也不静默退化成
// 另一个 provider（跑出来的证据要能说清它是在哪儿产生的）。
func TestPolarProviderRefusesUnconfiguredAndEmptyBaseline(t *testing.T) {
	unconfigured := &PolarProvider{}
	if _, err := unconfigured.ProvisionBranch(context.Background(), "baseline", "abc123"); err == nil {
		t.Fatal("BranchDSN 为空时必须报错")
	} else if !strings.Contains(err.Error(), "未配置") {
		t.Fatalf("错误信息要说清原因：%v", err)
	}
	configured := &PolarProvider{BranchDSN: "postgres://user:pw@127.0.0.1:5432/postgres"}
	if _, err := configured.ProvisionBranch(context.Background(), "", "abc123"); err == nil {
		t.Fatal("没给业务数据基线时必须报错（空库不算验证）")
	} else if !strings.Contains(err.Error(), "业务数据基线") {
		t.Fatalf("错误信息要说清原因：%v", err)
	}
	if err := configured.CleanupBranch(context.Background(), "production_business_db"); err == nil {
		t.Fatal("必须拒绝删除非分支库（只认 branch_ 前缀）")
	}
}

// 真机制验证：把 PolarProvider 指向一个真实的 PostgreSQL 端点（测试库的维护库），
// 从**业务数据基线库**克隆分支、在分支上跑迁移（含一条失败语句，失败也要留证据）、
// 再删掉分支。
//
// 说明：PolarDB 是 PostgreSQL 兼容的，这条路径用的就是同一套
// `CREATE DATABASE ... TEMPLATE`；provider 名如实标成 polardb-agentic-branch，
// 与 LocalProvider 的结果不互相冒充。测试库只用来验证机制，不代表跑在 PolarDB 上。
func TestPolarProviderProvisionsFromBusinessBaseline(t *testing.T) {
	_, dsn := testdb.OpenWithURL(t)
	ctx := context.Background()
	adminDSN := replaceDatabase(dsn, "postgres")
	baseline := "baseline_" + polarSuffix(t)
	t.Cleanup(func() { _ = execOn(context.Background(), adminDSN, `DROP DATABASE IF EXISTS "`+baseline+`"`) })
	if err := execOn(ctx, adminDSN, `CREATE DATABASE "`+baseline+`"`); err != nil {
		t.Fatalf("建基线库: %v", err)
	}
	baselineDSN := replaceDatabase(dsn, baseline)
	// 基线里是**业务数据**：一条历史订单（这正是空库测不出来的东西）。
	if err := execOn(ctx, baselineDSN,
		`CREATE TABLE quotes (id serial PRIMARY KEY, subtotal integer NOT NULL);
		 INSERT INTO quotes (subtotal) VALUES (880), (950);`); err != nil {
		t.Fatalf("造业务数据基线: %v", err)
	}

	provider := &PolarProvider{BranchDSN: adminDSN, ClusterID: "local-test-endpoint"}
	if provider.Name() != "polardb-agentic-branch" {
		t.Fatalf("provider 名 = %q", provider.Name())
	}
	branch, err := provider.ProvisionBranch(ctx, baseline, "abc123def456")
	if err != nil {
		t.Fatalf("ProvisionBranch: %v", err)
	}
	t.Cleanup(func() { _ = provider.CleanupBranch(context.Background(), branch) })

	// 分支里必须**带着基线的业务数据**（不是空库）。
	var rows int
	conn, err := pgx.Connect(ctx, replaceDatabase(dsn, branch))
	if err != nil {
		t.Fatalf("连分支库: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM quotes`).Scan(&rows); err != nil {
		t.Fatalf("查分支数据: %v", err)
	}
	_ = conn.Close(ctx)
	if rows != 2 {
		t.Fatalf("分支里的基线数据 = %d 行，应为 2（空库不算验证）", rows)
	}

	// 迁移：成功的一条 + 失败的一条，两条都要留结果。
	results, err := provider.ApplyMigrations(ctx, branch, []string{
		`ALTER TABLE quotes ADD COLUMN free_shipping boolean NOT NULL DEFAULT false`,
		`SELECT * FROM table_that_does_not_exist`,
	})
	if err == nil {
		t.Fatal("失败语句必须让 ApplyMigrations 报错")
	}
	if len(results) != 2 || !results[0].OK || results[1].OK || results[1].Error == "" {
		t.Fatalf("迁移证据不完整：%+v", results)
	}

	// 清理：分支库应当真的消失（直接查 pg_database，比"连一下试试"更直白）。
	if err := provider.CleanupBranch(ctx, branch); err != nil {
		t.Fatalf("CleanupBranch: %v", err)
	}
	exists, err := databaseExists(ctx, adminDSN, branch)
	if err != nil {
		t.Fatalf("查 pg_database: %v", err)
	}
	if exists {
		t.Fatalf("分支库 %s 应当已被删除", branch)
	}
	// 说明：这里**不再**用"连一下试试"来判断库还在不在 —— 那个判据在 pgx 下会给出
	// 误导性结论（连接一个已被删除的库名时它并不总是报错）。权威判据是 pg_database，
	// 上面那一查就是。
	_ = dsn
}

// databaseExists 直接问服务端这个库还在不在。
func databaseExists(ctx context.Context, adminDSN, name string) (bool, error) {
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, name).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
