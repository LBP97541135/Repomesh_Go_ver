package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这条读面**读文件**，而文件路径来自库里的 `agent_runs.workspace`。所以路径校验
// 是它的安全边界，必须有测试钉住 —— 尤其是"前缀相同但不是子目录"这一类：
// `/opt/repomesh/workspaces-evil` 用 strings.HasPrefix 判会通过。
func TestWithinRoot(t *testing.T) {
	root := "/opt/repomesh/workspaces"
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"根目录自身", root, true},
		{"根目录下的工作区", root + "/att_dag_abc", true},
		{"更深一层", root + "/att_dag_abc/repo", true},
		{"同前缀但不是子目录", root + "-evil", false},
		{"同前缀但不是子目录（带内容）", root + "-evil/att_dag_abc", false},
		{"完全在别处", "/etc", false},
		{"上跳", root + "/../etc", false},
		{"相对路径拼出来的上跳", root + "/att/../../etc", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := withinRoot(filepath.Clean(root), filepath.Clean(tc.path)); got != tc.want {
				t.Fatalf("withinRoot(%q) = %v，期望 %v", tc.path, got, tc.want)
			}
		})
	}
}

// tailWorkspaceLog 的三件事：只回尾部、如实报"截断过"、以及**挡住越界路径**。
func TestTailWorkspaceLog(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REPOMESH_WORKSPACE_ROOT", root)
	ws := filepath.Join(root, "att_dag_test")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}

	// 一份比 tail 大得多的日志：**开头**放一个只在开头出现的标记，
	// 结尾放要看的东西。断言"标记不在尾部里"才能证明读的是尾部而不是全文。
	// （不能靠断言"不以 noise 开头"—— 尾部 1024 字节本来就全是 noise 行。）
	body := "FIRSTLINE-ONLY-AT-HEAD\n" + strings.Repeat("noise\n", 400) + "最后的结论：改好了\n"
	if err := os.WriteFile(filepath.Join(ws, "agent-stdout.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	text, truncated, err := tailWorkspaceLog(ws, "agent-stdout.log", 1024)
	if err != nil {
		t.Fatalf("读尾部失败：%v", err)
	}
	if !truncated {
		t.Fatal("文件比 tail 大，truncated 应为 true（如实告诉界面这是尾部）")
	}
	if !strings.Contains(text, "最后的结论") {
		t.Fatalf("尾部应当包含结尾内容，实得 %q", text)
	}
	if strings.Contains(text, "FIRSTLINE-ONLY-AT-HEAD") {
		t.Fatal("尾部里不该出现只存在于文件开头的内容 —— 说明读的是全文而不是尾部")
	}
	if len(text) > 1024 {
		t.Fatalf("尾部长度 %d 超过请求的 1024", len(text))
	}

	// 小文件不截断。
	if err := os.WriteFile(filepath.Join(ws, "agent-stderr.log"), []byte("短的\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if text, truncated, err = tailWorkspaceLog(ws, "agent-stderr.log", 4096); err != nil || truncated || text != "短的\n" {
		t.Fatalf("小文件应原样返回且不截断，实得 %q truncated=%v err=%v", text, truncated, err)
	}

	// 越界的工作区：直接拒绝（而不是"读不到就当空"）。
	if _, _, err := tailWorkspaceLog(filepath.Join(root, "..", "etc"), "agent-stdout.log", 1024); err == nil {
		t.Fatal("工作区在根目录之外时必须报错")
	}
	// 白名单之外的文件名：拒绝。请求里能控制的任何东西都不许拼进路径。
	if _, _, err := tailWorkspaceLog(ws, "../../etc/passwd", 1024); err == nil {
		t.Fatal("白名单外的文件名必须报错")
	}
	// 空工作区：报错（调用方据此如实记进 logs_missing，不假装"没干活"）。
	if _, _, err := tailWorkspaceLog("", "agent-stdout.log", 1024); err == nil {
		t.Fatal("空工作区必须报错")
	}
}
