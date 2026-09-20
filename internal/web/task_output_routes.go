package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/access"
)

// registerTaskOutputRoutes 暴露「这条任务到底干了什么」的读面。
//
// 2026-09-20 用户反馈（反复提过几次）："右边的流式输出里，看不到 worker 具体的
// 工作内容"、"我也看不到 worker 的内部工作记录"。查下来是**根本没有读面**：
//
//   · `public.log_entries` 全仓没有任何生产者（线上实测 0 行），观测日志面是死路；
//   · 真正的产出在磁盘上 —— `/opt/repomesh/workspaces/<attempt>/agent-stdout.log`
//     （线上实测单个 20KB，agent 自己写的变更说明就在里面）与 `agent-stderr.log`；
//   · 库里的连接点是 `repomesh_execution.agent_runs.task_package_ref`，
//     dev run 与 test run 都**直接用 task id** 当这个 ref。
//
// 所以这里按 as-built 形状补一个薄读面：不建新表、不编内容，读不到就如实说
// 读不到（workspace 被清掉、日志还没落盘都如实反映），不假装有。
//
// 安全：路径**不来自请求**，来自库里的 `agent_runs.workspace`；即便如此仍然
// 逐层校验（Clean + 必须在工作区根目录之下 + EvalSymlinks 后仍在根目录之下），
// 因为"读一个由数据决定的文件"本身就是攻击面。
func registerTaskOutputRoutes(mux *http.ServeMux, auth Auth, pipeline Pipeline) {
	if pipeline.Pool == nil {
		return
	}
	pool := pipeline.Pool
	mux.HandleFunc("GET /api/projects/{projectId}/tasks/{taskId}/agent-output", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil {
			writeProjectError(w, &access.Failure{Status: http.StatusServiceUnavailable, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		claims, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		// 会话事实校验：RecheckProjectPrincipal 自带事务，与 registerProjectRoute
		// 走的是同一批校验（绑定/会话/账号未吊销未过期）。
		if err := auth.Service.RecheckProjectPrincipal(ctx, claims); err != nil {
			writeProjectError(w, err)
			return
		}
		projectID := r.PathValue("projectId")
		// 再叠一层**归属**校验：这个项目得属于该账号的组织。
		//
		// 为什么必须加：`agent_runs.task_package_ref` 是**全局字符串**，project_id
		// 又来自**请求路径**——只按路径过滤的话，任何人拿别人的 (projectId, taskId)
		// 就能读到别人的 agent 日志。这是本读面独有的风险（其它读面读的是自己
		// 项目域内、由 service 层二次校验过的数据），所以这里显式挡一道。
		ok, err := projectBelongsToActorOrg(ctx, pool, projectID, claims.ActorID())
		if err != nil {
			writeProjectError(w, err)
			return
		}
		if !ok {
			writeProjectError(w, &access.Failure{Status: http.StatusNotFound, Code: "PROJECT_NOT_FOUND"})
			return
		}
		view, err := readTaskAgentOutput(ctx, pool, projectID, r.PathValue("taskId"), outputTailBytes(r))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
}

// projectBelongsToActorOrg 判断项目是否属于该账号所在的组织。
//
// 口径与库里的既有事实一致：`repomesh_projects.projects.owner` 指向
// `repomesh_access.accounts.id`（**都是 text**），账号带 `organization_id`。
// 判不出来（项目不存在/账号不存在）一律当"不属于" —— 读面不区分
// "不存在"和"没权限"，免得把别人的项目存在性当情报漏出去。
func projectBelongsToActorOrg(ctx context.Context, pool taskOutputQuerier, projectID, actor string) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM repomesh_projects.projects p
		JOIN repomesh_access.accounts owner ON owner.id = p.owner
		WHERE p.id = $1
		  AND owner.organization_id = (SELECT organization_id FROM repomesh_access.accounts WHERE id = $2)
	)`, projectID, actor).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// taskOutputTailDefault 是每份日志默认回多少字节的**尾部**。
//
// 回尾部而不是全文：agent 的 stdout 是流式的、可能几 MB，而人要看的"它到底
// 干了什么"几乎总在结尾（总结 / 失败原因 / exit code）。上限 256KB 免得一个
// 请求把内存和响应都撑爆。
const taskOutputTailDefault = 64 * 1024

func outputTailBytes(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("tail"))
	if raw == "" {
		return taskOutputTailDefault
	}
	n := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return taskOutputTailDefault
		}
		n = n*10 + int(ch-'0')
		if n > 256*1024 {
			return 256 * 1024
		}
	}
	if n < 1024 {
		return taskOutputTailDefault
	}
	return n
}

// workspaceRootForRead 读环境变量（部署与协调器同一约定），缺省用线上实际路径。
func workspaceRootForRead() string {
	if v := strings.TrimSpace(os.Getenv("REPOMESH_WORKSPACE_ROOT")); v != "" {
		return v
	}
	return "/opt/repomesh/workspaces"
}

type taskRunOutput struct {
	RunID      string `json:"run_id"`
	AgentKind  string `json:"agent_kind"`
	State      string `json:"state"`
	ExitCode   *int   `json:"exit_code"`
	Workspace  string `json:"workspace"`
	Repo       string `json:"repo_full_name"`
	StartedAt  string `json:"started_at"`
	ExitedAt   string `json:"exited_at"`
	StdoutTail string `json:"stdout_tail"`
	StderrTail string `json:"stderr_tail"`
	// StdoutTruncated / StderrTruncated 如实说明"你看到的是尾部"。
	StdoutTruncated bool `json:"stdout_truncated"`
	StderrTruncated bool `json:"stderr_truncated"`
	// LogsMissing 列出**读不到**的那几份日志（工作区被清掉 / 还没落盘），
	// 让界面能如实说"没有记录"而不是显示一片空白让人以为"它什么都没干"。
	LogsMissing []string `json:"logs_missing"`
}

type taskAgentOutputView struct {
	TaskID string          `json:"task_id"`
	Runs   []taskRunOutput `json:"runs"`
	// WorkspaceRoot 一并回出去，便于排障时对得上"读的是哪个目录"。
	WorkspaceRoot string `json:"workspace_root"`
}

type taskOutputQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// readTaskAgentOutput 读一条任务名下的**全部** agent run（dev + test + 集成），
// 每个 run 带上工作区里日志的尾部。
//
// 为什么按 `task_package_ref` 而不是按 attempt：`attempts` 表**没有 task_id 列**
// （列是 id/project_id/issue_id/worker_id/…），而 `agent_runs.task_package_ref`
// 对 dev run 与 test run 都**直接用 task id**（线上实测：`run_dag_…` 与
// `run_test_…` 的 ref 逐字等于 `public.tasks.id`）。规划 run 用的是
// `planning:<issueId>:<step>`、集成 run 用 `plan:<planId>:kind:…`，都**不会**
// 等于一个 task id，所以这个过滤天然只命中任务自己的 run。
//
// 仍然 join `attempts` 取 project_id：读面必须按项目授权，而 task_package_ref
// 是全局字符串，单靠它挡不住"拿别人的 task id 来读"。
func readTaskAgentOutput(ctx context.Context, pool taskOutputQuerier, projectID, taskID string, tail int) (taskAgentOutputView, error) {
	view := taskAgentOutputView{TaskID: taskID, Runs: []taskRunOutput{}, WorkspaceRoot: workspaceRootForRead()}
	rows, err := pool.Query(ctx, `
		SELECT r.id::text, r.agent_kind, r.state, r.exit_code, COALESCE(r.workspace,''),
		       COALESCE(r.repo_full_name,''),
		       COALESCE(to_char(r.started_at,'YYYY-MM-DD HH24:MI:SS'),''),
		       COALESCE(to_char(r.exited_at,'YYYY-MM-DD HH24:MI:SS'),'')
		FROM repomesh_execution.agent_runs r
		JOIN repomesh_execution.attempts a ON a.id = r.attempt_id
		-- 不要写成 $1::uuid：repomesh_execution.attempts.project_id 是 text
		-- （库里的项目标识有两套口径，见 0057 那次 text/uuid 选型事故），
		-- 加了 uuid 转换就会 operator does not exist: text = uuid（SQLSTATE 42883）。
		WHERE a.project_id = $1 AND r.task_package_ref = $2
		ORDER BY r.created_at`, projectID, taskID)
	if err != nil {
		return taskAgentOutputView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var run taskRunOutput
		if err := rows.Scan(&run.RunID, &run.AgentKind, &run.State, &run.ExitCode,
			&run.Workspace, &run.Repo, &run.StartedAt, &run.ExitedAt); err != nil {
			return taskAgentOutputView{}, err
		}
		run.LogsMissing = []string{}
		run.StdoutTail, run.StdoutTruncated, err = tailWorkspaceLog(run.Workspace, "agent-stdout.log", tail)
		if err != nil {
			run.LogsMissing = append(run.LogsMissing, "agent-stdout.log")
			run.StdoutTail = ""
		}
		run.StderrTail, run.StderrTruncated, err = tailWorkspaceLog(run.Workspace, "agent-stderr.log", tail)
		if err != nil {
			run.LogsMissing = append(run.LogsMissing, "agent-stderr.log")
			run.StderrTail = ""
		}
		view.Runs = append(view.Runs, run)
	}
	return view, rows.Err()
}

// tailWorkspaceLog 读工作区里某份日志的尾部，带路径校验。
//
// 三层校验，缺一不可：
//  1. 工作区路径**必须**在工作区根目录之下（Clean 后按目录前缀比，不是字符串
//     HasPrefix —— `/opt/repomesh/workspaces-x` 也能前缀命中 `/opt/repomesh/workspaces`）；
//  2. EvalSymlinks 之后再比一次（防"根目录下的一个软链指向 /etc"）；
//  3. 只读白名单里的文件名，不拼任何请求里的东西。
//
// 读不到就返回错误，由调用方如实记进 logs_missing —— **不假装空日志等于没干活**。
func tailWorkspaceLog(workspace, name string, tail int) (string, bool, error) {
	if workspace == "" {
		return "", false, errors.New("workspace is empty")
	}
	if name != "agent-stdout.log" && name != "agent-stderr.log" {
		return "", false, errors.New("log name not allowed")
	}
	root, err := filepath.Abs(workspaceRootForRead())
	if err != nil {
		return "", false, err
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootResolved = root
	}
	dir := filepath.Clean(workspace)
	if !withinRoot(rootResolved, dir) {
		return "", false, errors.New("workspace outside root")
	}
	dirResolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false, err
	}
	if !withinRoot(rootResolved, dirResolved) {
		return "", false, errors.New("workspace escapes root via symlink")
	}
	path := filepath.Join(dirResolved, name)
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, err
	}
	size := info.Size()
	start := int64(0)
	truncated := false
	if size > int64(tail) {
		start = size - int64(tail)
		truncated = true
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", false, err
	}
	text := string(data)
	// 从字节中间切开会把多字节字符截成半个 —— 丢掉开头那个残字。
	if truncated {
		if index := strings.IndexByte(text, '\n'); index >= 0 {
			text = text[index+1:]
		}
	}
	return text, truncated, nil
}

// withinRoot 判断 path 是否就在 root 之下（含 root 自身）。
//
// 用 filepath.Rel 而不是 strings.HasPrefix：后者会把
// `/opt/repomesh/workspaces-evil` 判成"在根目录之下"。
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}
