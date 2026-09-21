package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/branchvalidation"
	"repomesh.local/repomesh/internal/buildinfo"
	"repomesh.local/repomesh/internal/console"
	"repomesh.local/repomesh/internal/database"
	"repomesh.local/repomesh/internal/decisionchain"
	"repomesh.local/repomesh/internal/deliverymanifest"
	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/gates"
	"repomesh.local/repomesh/internal/handoff"
	"repomesh.local/repomesh/internal/humancontrol"
	"repomesh.local/repomesh/internal/interfacedoc"
	"repomesh.local/repomesh/internal/issues"
	"repomesh.local/repomesh/internal/jointvalidation"
	"repomesh.local/repomesh/internal/manager"
	"repomesh.local/repomesh/internal/messages"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/observepipe"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/reposcan"
	"repomesh.local/repomesh/internal/repositoryteams"
	"repomesh.local/repomesh/internal/responsibility"
	"repomesh.local/repomesh/internal/roomnotice"
	"repomesh.local/repomesh/internal/scan"
	"repomesh.local/repomesh/internal/scm"
	"repomesh.local/repomesh/internal/secrets"
	skills "repomesh.local/repomesh/internal/skills"
	"repomesh.local/repomesh/internal/spec"
	"repomesh.local/repomesh/internal/tasks"
	"repomesh.local/repomesh/internal/typesafe"
	"repomesh.local/repomesh/internal/web"
)

func main() {
	os.Exit(mainExit())
}

func mainExit() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

// configureModelBudgets wires the project quota surface to the modelbudget
// store. projects never imports modelbudget; nil means unknown, never zero.
// Both sides address windows by (project_model_runtime scope, UTC midnight) —
// the same key ReserveTest reserves under, so observations and reservations
// always describe the same ledger.
func configureModelBudgets(projectService *projects.Service, budgets *modelbudget.Store) {
	scope := func(scopeID string, now time.Time) modelbudget.WindowID {
		day := now.UTC().Truncate(24 * time.Hour)
		return modelbudget.WindowID{ScopeKind: "project_model_runtime", ScopeID: scopeID, StartUTC: day}
	}
	projectService.SetRequestQuotaObserver(func(ctx context.Context, tx pgx.Tx, scopeID string, revision projects.ConfigurationRevision, policy projects.RequestPolicy, now time.Time) (projects.QuotaObservation, error) {
		observation, err := budgets.Observe(ctx, tx, scope(scopeID, now), policy)
		if err != nil {
			return projects.UnknownQuotaObservation(revision, []string{"quota_observation_failed"}, nil), err
		}
		switch known := observation.(type) {
		case modelbudget.Known:
			return projects.KnownQuotaObservation(revision, projects.KnownQuota{
				Policy:         policy.Ref,
				WindowStart:    known.Window.ID.StartUTC,
				WindowEnd:      known.Window.EndUTC,
				ObservedAt:     known.At,
				EffectiveLimit: known.EffectiveLimit,
				Reserved:       known.Window.Reserved,
				Consumed:       known.Window.Consumed,
				Remaining:      known.Remaining,
			}), nil
		case modelbudget.Unknown:
			return projects.UnknownQuotaObservation(revision, known.Reasons, known.At), nil
		default:
			return projects.UnknownQuotaObservation(revision, []string{"quota_observation_failed"}, nil), nil
		}
	})
	projectService.SetRequestWindowInitializer(func(ctx context.Context, tx pgx.Tx, scopeID string, policy projects.RequestPolicy) error {
		windowScope := scope(scopeID, time.Now())
		evidence, err := budgets.CheckEmptyWindowHistory(ctx, tx, windowScope)
		if err != nil {
			return err
		}
		_, err = budgets.EnsureProjectWindow(ctx, tx, windowScope, policy, evidence)
		return err
	})
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "db" {
		return runDatabase(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "sources" {
		return runSources(ctx, args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("repomesh-web", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", envOr("REPOMESH_WEB_ADDR", "127.0.0.1:8080"), "HTTP listen address")
	assets := flags.String("assets", envOr("REPOMESH_WEB_ASSETS", "web/dist"), "built frontend directory (relative to working directory)")
	version := flags.Bool("version", false, "print release version and exit")
	authConfig := flags.String("auth-config", os.Getenv("REPOMESH_AUTH_CONFIG"), "authentication deployment JSON file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Fprintf(stdout, "repomesh-web %s\n", buildinfo.Version)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	var auth web.Auth
	var projectAPI web.Projects
	var modelAPI web.Models
	var scanAPI web.Scan
	// scanAdapter 是同一个适配器的句柄：升级梯 onboarding 要**内部触发扫描**，
	// 那条路径不经过 HTTP 会话，拿不到 web.Scan 的 RegisterRoutes 之外的东西。
	var scanAdapter *scan.HTTP
	var decisionAPI web.Decision
	var skillsAPI web.Skills
	var issuesAPI web.Issues
	var messagesAPI web.Messages
	var pipelineAPI web.Pipeline
	var atClient *agentteams.Client
	// The pipeline services need a pool even in unauthenticated mode so their
	// routes exist; DB failures surface as 503 from the handlers themselves.
	var certFile, keyFile string
	// M1-M9 pipeline services run on their own pool so the routes exist in
	// every deployment mode; handlers surface DB errors as 503 honestly.
	var pipelinePool *pgxpool.Pool
	// secretStore 由认证运行时提供；候选召回（语义匹配）要解封模型供应商密钥。
	// 未启用认证时保持 nil —— discovery 会如实回退到关键词路径。
	var secretStore *secrets.Store
	if dsn := os.Getenv("REPOMESH_DATABASE_URL"); dsn != "" {
		if pool, poolErr := pgxpool.New(ctx, dsn); poolErr == nil {
			pipelinePool = pool
			defer pipelinePool.Close()
		} else {
			fmt.Fprintln(stderr, "pipeline pool unavailable (routes will 503):", poolErr)
		}
	}
	// 交付段的"能合并的手"：auth 块里才有 access.Service（GitHub App 客户端），
	// 而它要在下面的 pipeline 装配里用到，所以在函数顶层先占个位。
	// 没配 auth-config（或没配 App 凭据）时保持 nil —— 合并端点会如实回
	// "服务端没有可用的 GitHub 凭据"，不假装能合。
	var scmMerger scm.PullMerger
	if *authConfig != "" {
		startup, cancel := context.WithTimeout(ctx, 30*time.Second)
		runtime, err := access.OpenRuntime(startup, *authConfig, os.Getenv("REPOMESH_DATABASE_URL"))
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "authentication startup:", err)
			return 1
		}
		defer runtime.Close()
		secretStore = runtime.SecretStore()
		// 注意别把 typed-nil 装进接口：那样 scm.Merge 的 nil 检查会失效。
		if client := runtime.Service.GitHubAppClient(); client != nil {
			scmMerger = client
		}
		// 认证域后台 worker:发现批次上游抓取、凭据轮换与过期清理。
		// web 部署形态此前没接这条循环,候选仓库(发现仓库段)永远为空。
		// RunOne 内部有租约/SKIP LOCKED,与 coordinator 并发安全。
		go func() {
			for {
				workCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				worked, err := runtime.Service.RunOne(workCtx)
				cancel()
				if err != nil {
					slog.Warn("authentication work deferred", "reason", err.Error())
				}
				if !worked {
					time.Sleep(2 * time.Second)
				}
			}
		}()
		auth = web.Auth{Service: runtime.Service, Origin: runtime.Deployment.Origin}
		projectService := projects.New(runtime.Pool(), runtime.Service)
		runtime.Service.SetProjectDestinationResolver(projectService.ResolveDestination)
		projectAPI = web.Projects{Service: projectService}
		catalog := projects.NewCatalogWriter()
		modelService := models.New(runtime.Pool(), runtime.Service, runtime.SecretStore(), catalog)
		runtime.Service.SetModelSaveDestinationResolver(modelService.ResolveDestination)
		// B05: budgeted model tests and project-scoped model applications.
		// The budget store never blocks startup — windows initialize lazily on
		// the first reservation, so a fresh database is simply a zero-quota
		// observation, not a boot failure.
		budgets := modelbudget.New()
		configureModelBudgets(projectService, budgets)
		testService := models.NewTestService(runtime.Pool(), runtime.Service, runtime.SecretStore(), budgets)
		applyService := models.NewApplicationService(runtime.Pool(), runtime.Service, projectService, runtime.SecretStore())
		runtime.Service.SetModelTestDestinationResolver(testService.ResolveDestination)
		runtime.Service.SetModelApplyDestinationResolver(applyService.ResolveDestination)
		modelAPI = web.Models{Service: modelService, Tests: testService, Applications: applyService, TypeSafe: typesafe.New(runtime.Pool(), runtime.SecretStore())}
		// B06: atomic issue creation shares the pool, authorization surface and
		// budget store; its destination resolver maps an issue_create operation
		// destination back to the browser creation page.
		issueService := issues.New(runtime.Pool(), runtime.Service, projectService, budgets)
		runtime.Service.SetIssueCreationDestinationResolver(issueService.ResolveDestination)
		issuesAPI = web.Issues{Service: issueService}
		// B09: conversation message persistence and per-issue configuration
		// consumption. The transport adapters stay unconfigured here; their
		// absence keeps delivery blocked instead of faking a Manager round trip.
		messageService := messages.NewMessageService(runtime.Pool(), runtime.Service, issueService)
		consumptionService := manager.New(runtime.Pool(), projectService)
		messagesAPI = web.Messages{Service: messageService, Consumption: consumptionService}
		// M1-M9 pipeline services share the runtime pool; nil sub-services
		// keep their routes responding 503 SERVICE_NOT_CONFIGURED honestly.
		if atClient == nil {
			atClient = &agentteams.Client{
				BaseURL: strings.TrimRight(os.Getenv("AGENTTEAMS_CONTROLLER_URL"), "/"),
				Token:   os.Getenv("AGENTTEAMS_CONTROLLER_TOKEN"),
			}
			if atClient.BaseURL == "" {
				atClient = nil
			}
		}
		// 房间消息走 AgentTeams 的 homeserver：控制器的 REST 里没有"按 roomID 读消息"
		// 这条（只有 /projects/{id}/spawns/{sessionId}/messages，要求先有项目，而
		// RepoMesh 不建项目）。凭据现换现用 —— 见 agentteams.MatrixSession。
		//
		// 缺任一环境变量就不装：房间消息端点返 503「没配」，而不是返空消息流让界面
		// 以为"房间没人说话"。房间**关联**不受影响，那部分只读本库。
		// 房间凭据两条路：直配 MATRIX_ACCESS_TOKEN 优先，控制器换凭据兜底
		//（admin 在控制器那边没有凭据记录，换必 500 —— 见 NewSessionFromEnv）。
		if session := agentteams.NewSessionFromEnv(atClient); session != nil {
			issuesAPI.Matrix = session
			// 建项成功 → 把"收到新需求"投进团队房。同一套环境变量，
			// 缺了就是 nil，Notify 静默空操作。
			issuesAPI.Rooms = roomnotice.NewFromEnv(runtime.Pool())
		}
		pipelineAPI.HandoffDocs = web.HandoffDocs{Service: handoff.New(runtime.Pool())}
		pipelineAPI.Extensions = web.PipelineExtensions{
			Spec:  spec.New(runtime.Pool()),
			Gates: gates.New(runtime.Pool()),
		}
		pipelineAPI.JointValidation = web.JointValidation{Service: jointvalidation.New(runtime.Pool())}
		pipelineAPI.SCMRoutes = web.PipelineSCM{
			SCM:         scm.New(runtime.Pool(), os.Getenv("REPOMESH_WEBHOOK_SECRET")),
			Merger:      scmMerger,
			Observation: observability.New(runtime.Pool()),
		}
		certFile, keyFile = runtime.Deployment.TLSCertificateFile, runtime.Deployment.TLSKeyFile

		// Scan block: repository scanning + scope selection (D5/D8, design
		// doc 仓库扫描终版设计). Fetcher per platform via the router; the
		// catalog lives in the same PostgreSQL database.
		scanCatalog := scan.NewPostgresCatalog(runtime.Pool())
		scanService := scan.New(scan.Config{
			ScopeAssistEnabled: envBool("REPOMESH_SCOPE_ASSIST_ENABLED", true),
		}, scanCatalog)
		for _, channel := range scan.DefaultChannels() {
			scanService.RegisterChannel(channel)
		}
		// 历史决策 module (design: 历史决策终版设计, D9-D14): scope
		// confirmations become decision nodes. Embedding config presence
		// toggles semantic recall; without it recall degrades to structural.
		decisionService := decisionchain.New(decisionchain.Config{
			EmbeddingBaseURL: os.Getenv("REPOMESH_EMBEDDING_BASE_URL"),
			EmbeddingAPIKey:  os.Getenv("REPOMESH_EMBEDDING_API_KEY"),
			EmbeddingModel:   os.Getenv("REPOMESH_EMBEDDING_MODEL"),
			ResolveName: func(ctx context.Context, id string) (string, bool) {
				card, err := scanCatalog.Get(ctx, id)
				if err != nil || card == nil {
					return "", false
				}
				return card.Name, true
			},
		}, runtime.Pool())
		// Same guard convention as the scan block: reads stay open, writes
		// require Origin + session + CSRF.
		decisionService.Authenticate = func(r *http.Request) error {
			if r.Method == http.MethodGet {
				return nil
			}
			if runtime.Deployment.Origin == "" || r.Header.Get("Origin") != runtime.Deployment.Origin {
				return errors.New("origin rejected")
			}
			_, err := runtime.Service.AuthenticateProjectRequest(
				r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), true)
			return err
		}

		decisionService.ActorName = func(r *http.Request) string {
			if principal, err := runtime.Service.AuthenticateProjectRequest(
				r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), false); err == nil {
				return principal.ActorID()
			}
			return ""
		}
		// 读隔离：列表 / 相似 / 语义检索都只返回该账号自己项目下的决策节点。
		// 当前阶段所有账号同等对待、无管理员放权（2026-09-20 用户裁定）。
		decisionService.ActorID = func(r *http.Request) string {
			if principal, err := runtime.Service.AuthenticateProjectRequest(
				r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), false); err == nil {
				return principal.ActorID()
			}
			return ""
		}
		decisionAPI = web.Decision{API: decisionService}

		// Skill governance block (capability_management plugin ported to Go):
		// 15 seeded SKILL.md presets, tiered approvals, AB blind evaluation,
		// MCP call policies. Seeds are idempotent and non-blocking.
		skillStore := &skills.Store{Pool: runtime.Pool()}
		if err := skills.SeedMcpPolicies(ctx, skillStore); err != nil {
			fmt.Fprintf(stderr, "seed mcp policies (non-blocking): %v\n", err)
		}
		if err := skills.SeedSkills(ctx, skillStore, "system-seed"); err != nil {
			fmt.Fprintf(stderr, "seed skills (non-blocking): %v\n", err)
		}
		skillService := skills.NewService(skillStore)
		// 2026-09-19 账号隔离：技能库按**调用者自己的空间**裁剪（迁移 0039）。
		// 解析失败一律返回空串 —— 那时只认全局种子技能，宁可少给，也不越权多给。
		skillService.ActorOrganization = func(r *http.Request) string {
			principal, err := runtime.Service.AuthenticateProjectRequest(
				r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), false)
			if err != nil {
				return ""
			}
			organization, err := runtime.Service.OrganizationOf(r.Context(), principal.ActorID())
			if err != nil {
				return ""
			}
			return organization
		} // Same guard convention as the decision block: writes require Origin +
		// session + CSRF; GET reads stay open.
		skillService.Authenticate = func(r *http.Request) error {
			if r.Method == http.MethodGet {
				return nil
			}
			if runtime.Deployment.Origin == "" || r.Header.Get("Origin") != runtime.Deployment.Origin {
				return errors.New("origin rejected")
			}
			_, err := runtime.Service.AuthenticateProjectRequest(
				r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), true)
			return err
		}
		skillService.ActorName = func(r *http.Request) string {
			if principal, err := runtime.Service.AuthenticateProjectRequest(
				r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), false); err == nil {
				return principal.ActorID()
			}
			return ""
		}
		skillsAPI = web.Skills{API: skillService}

		fetcher := &reposcan.Router{
			GitHub: &reposcan.GitHubFetcher{Token: os.Getenv("REPOMESH_REPOSITORY_SCAN_GITHUB_TOKEN")},
			GitLab: &reposcan.GitLabFetcher{Token: os.Getenv("REPOMESH_REPOSITORY_SCAN_GITLAB_TOKEN")},
			Extra:  parsePlatformMap(os.Getenv("REPOMESH_REPOSITORY_SCAN_PLATFORMS")),
		}
		scanAdapter = &scan.HTTP{
			Service:       scanService,
			Store:         scanCatalog,
			Runner:        &scan.Runner{Fetcher: fetcher, Store: scanCatalog, IncludeForks: envBool("REPOMESH_REPOSITORY_SCAN_INCLUDE_FORKS", false)},
			Jobs:          scan.NewJobRegistry().WithMirror(scan.NewJobMirror(pipelinePool)),
			Suggester:     scan.KeywordSuggester{Store: scanCatalog},
			Fetcher:       fetcher,
			Allowlist:     splitList(os.Getenv("REPOMESH_REPOSITORY_SCAN_ALLOWED_HOSTS")),
			PlatformExtra: parsePlatformMap(os.Getenv("REPOMESH_REPOSITORY_SCAN_PLATFORMS")),
			// 2026-09-19：扫描以**发起人本人**的 GitHub 令牌跑（5000 次/小时、
			// 按账号隔离），而不是部署级环境变量（未配置时退化为匿名 60 次/小时）。
			// 拿不到用户令牌时如实降级，绝不假装有凭据。
			ActorToken: func(r *http.Request) (string, error) {
				principal, err := runtime.Service.AuthenticateProjectRequest(
					r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), false)
				if err != nil {
					return "", err
				}
				return runtime.Service.UserGitHubToken(r.Context(), principal.ActorID())
			},
			// 2026-09-19 账号隔离：扫描目录按调用者的空间裁剪（读）与盖章（写）。
			ActorOrganization: func(r *http.Request) string {
				principal, err := runtime.Service.AuthenticateProjectRequest(
					r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), false)
				if err != nil {
					return ""
				}
				organization, err := runtime.Service.OrganizationOf(r.Context(), principal.ActorID())
				if err != nil {
					return ""
				}
				return organization
			},
			Authenticate: func(r *http.Request) error {
				if r.Method == http.MethodGet {
					return nil // reads stay open, matching the other catalog reads
				}
				if runtime.Deployment.Origin == "" || r.Header.Get("Origin") != runtime.Deployment.Origin {
					return errors.New("origin rejected")
				}
				_, err := runtime.Service.AuthenticateProjectRequest(
					r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), true)
				return err
			},
			OnScopeDecided: func(r *http.Request, d scan.ScopeDecision) {
				// Fail-open (方案清单 F3): a record failure must never fail
				// the user's scope submission. The log carries the payload
				// so a lost record can be backfilled by hand.
				actor := ""
				if principal, err := runtime.Service.AuthenticateProjectRequest(
					r.Context(), web.SessionCookie(r), r.Header.Get("X-CSRF-Token"), true); err == nil {
					actor = principal.ActorID()
				}
				// WithoutCancel: a client disconnecting right after submit
				// must not orphan the audit record.
				err := decisionService.Record(context.WithoutCancel(r.Context()), decisionchain.Event{
					Requirement:    d.Requirement,
					Actor:          actor,
					IdempotencyKey: d.IdempotencyKey,
					RepositoryIDs:  d.RepositoryIDs,
					Accepted:       d.Accepted,
				})
				if errors.Is(err, decisionchain.ErrDisabled) {
					return // toggle off: silent no-op (D12), nothing to audit
				}
				if err != nil {
					fmt.Fprintf(stderr, "decision chain record failed: %v (requirement=%q ids=%v key=%s actor=%q)\n",
						err, d.Requirement, d.RepositoryIDs, d.IdempotencyKey, actor)
				}
			},
		}
		scanAPI = web.Scan{API: scanAdapter}
	}
	// M1-M9 pipeline assembly (own pool; works with or without auth-config).
	if pipelinePool != nil {
		// 数据库分支验证的 provider（评委①）：默认本地 PostgreSQL（同一实例 TEMPLATE
		// 克隆）；配了 PolarDB 端点就切到 PolarDB provider（同一套机制，连的是 PolarDB
		// 集群）。两者产生的证据各自标注 provider，谁也不冒充谁。
		// 未配置时明确报错而不是静默退化 —— 见 branchProvider()。
		pipelineAPI = web.Pipeline{
			Tasks:             tasks.NewPostgresStore(pipelinePool),
			Assembly:          assembly.New(pipelinePool, atClient, nil),
			InterfaceDoc:      interfacedoc.New(pipelinePool),
			BranchValid:       branchvalidation.New(pipelinePool, branchProvider()),
			Observation:       observability.New(pipelinePool),
			DeliveryManifests: deliverymanifest.New(pipelinePool),
			// Pool 给"计划快照"这类薄读面用（只读 public.plans 的 jsonb，不值得为它再造 service）。
			Pool: pipelinePool,
			Extensions: web.PipelineExtensions{
				Spec:  spec.New(pipelinePool),
				Gates: gates.New(pipelinePool),
			},
			JointValidation: web.JointValidation{Service: jointvalidation.New(pipelinePool)},
			SCMRoutes: web.PipelineSCM{
				SCM:         scm.New(pipelinePool, os.Getenv("REPOMESH_WEBHOOK_SECRET")),
				Merger:      scmMerger,
				Observation: observability.New(pipelinePool),
			},
			HandoffDocs: web.HandoffDocs{Service: handoff.New(pipelinePool)},
			// 升级梯（执行中人工打断 / 动态引入新仓库）。
			// 决策链那一侧**已经有落库实现**，这里补的是三个端口适配器
			// （见 escalation_adapters.go），不重复实现落库逻辑。
			Escalation: &tasks.EscalationService{
				Store:   tasks.NewPostgresStore(pipelinePool),
				Sink:    escalationSink{store: decisionchain.NewPostgresStore(pipelinePool)},
				Catalog: escalationCatalog{pool: pipelinePool},
				// 判定步骤的依赖邻接：接扫描域的依赖图（边来自观测到的运行时调用）。
				// 不接的话判定按"无邻接"处理，新增仓库永远不会被判为影响当前计划。
				Adjacency: escalationAdjacency{pool: pipelinePool},
				// onboarding：未扫描的仓库触发一次单仓扫描。**必须解析出组织** —— 扫描按组织
				// 盖章（repomesh_scan.repositories.organization_id），而扫描目录的读面按组织
				// 裁剪；用空 org 扫出来的仓库，用户在自己的目录里**根本看不见**，onboarding
				// 就成了"报成功但没用"。
				OnboardMissing: func(ctx context.Context, planID, repository string) {
					var organization string
					if err := pipelinePool.QueryRow(ctx, "SELECT p.organization_id::text FROM public.plans pl JOIN repomesh_projects.projects p ON p.id = pl.project_id::text WHERE pl.id = $1::uuid", planID).Scan(&organization); err != nil {
						fmt.Fprintf(stderr, "escalation: onboarding 解析组织失败 plan=%s repo=%s: %v\n", planID, repository, err)
						return
					}
					if _, err := scanAdapter.StartRepositoryScan(ctx, repository, organization); err != nil {
						fmt.Fprintf(stderr, "escalation: onboarding 触发扫描失败 repo=%s: %v\n", repository, err)
					}
				},
				Window: 30 * time.Second,
				// 人工打断要等 X 就绪（未扫描则先 onboarding，见 §3 触发特例）。
				// 等太久会把 HTTP 请求拖死，所以给 60s：超时按 ready=false 如实返回
				// （决策单已落，用户可在就绪后再次打断判定），不假装成功。
				InterruptWait: 60 * time.Second,
			},
		}
	}
	if atClient == nil {
		atClient = &agentteams.Client{
			BaseURL: strings.TrimRight(os.Getenv("AGENTTEAMS_CONTROLLER_URL"), "/"),
			Token:   os.Getenv("AGENTTEAMS_CONTROLLER_TOKEN"),
		}
		if atClient.BaseURL == "" {
			atClient = nil // 未配置 Controller:路由返回 503 如实说明
		}
	}
	// atClient stays nil when AgentTeams is not configured: the adapter
	// routes answer 503 SERVICE_NOT_CONFIGURED honestly.
	consoleAPI := web.Console{}
	discoveryAPI := web.Discovery{}
	humanControlAPI := web.HumanControl{}
	observeV1 := web.ObserveV1{}
	if pipelinePool != nil {
		humanControlAPI = web.HumanControl{Service: humancontrol.New(pipelinePool)}
		observeV1 = web.ObserveV1{Service: observability.New(pipelinePool)}
		web.SetAgentSettingsPool(pipelinePool)
		// 「仓库 Owner 确认 = 交付闸门的 review」这条桥（2026-09-21 用户裁定）。
		// 没接这条桥时：Owner 确认照常落库，但闸门的 review 那一项永远不亮，
		// 交付列车上点合并必然失败（线上 19 个有 PR 的变更集里 10 个卡在这里）。
		web.SetResponsibilityService(responsibility.New(pipelinePool).WithReviewRecorder(
			ownerConfirmReviewRecorder(pipelinePool, scm.New(pipelinePool, os.Getenv("REPOMESH_WEBHOOK_SECRET")))))
		// The discovery chain audits approval + materialize decisions into
		// the same decision chain as the scan scope seam (B3 wiring); its
		// embedding config mirrors the main decision service.
		// WithSecrets：候选召回要出站调模型做语义判断，需要解封供应商密钥。
		// 没有它时 discovery 会如实回退到关键词路径并标注 llm_used=false。
		discoveryService := discovery.New(pipelinePool).
			WithSecrets(secretStore).
			WithDecisions(decisionchain.New(decisionchain.Config{
				EmbeddingBaseURL: os.Getenv("REPOMESH_EMBEDDING_BASE_URL"),
				EmbeddingAPIKey:  os.Getenv("REPOMESH_EMBEDDING_API_KEY"),
				EmbeddingModel:   os.Getenv("REPOMESH_EMBEDDING_MODEL"),
			}, pipelinePool)).
			// 重排 v2 的落库端口：收集窗开完之后由 Leader agent 产出的 v2，经它落进
			// public.plans（全量快照替换 + 任务轴迁移 + 决策链同事务）。不接的话
			// 第 6 步会如实失败 —— 不假装计划已经重排。
			WithReplanner(replanAdapter{store: tasks.NewPostgresStore(pipelinePool)})
		if dir := os.Getenv("REPOMESH_OBSERVE_ARCHIVE"); dir != "" {
			source := os.Getenv("REPOMESH_OBSERVE_SOURCE_ID")
			if source == "" {
				fmt.Fprintln(stderr, "observation source ID is required when capture is enabled")
				return 1
			}
			recorder, captureErr := observepipe.NewModelRecorder(dir, source+"/web", func(reason string) { fmt.Fprintln(stderr, reason) })
			if captureErr != nil {
				fmt.Fprintln(stderr, "cannot initialize private observation archive")
				return 1
			}
			discoveryService.WithModelObserver(recorder.Observe)
			defer func() {
				closing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = recorder.Close(closing)
			}()
		}
		// Reviews：发现链的人工步骤（③ 分档审批 / ⑤ 物化确认）镜像成审核台的待审项，
		// 否则审核台读的 review_requests 恒空（它此前全仓没有生产者）。
		discoveryAPI = web.Discovery{Service: discoveryService, Maintenance: discovery.NewMaintenance(pipelinePool), Reviews: humanControlAPI.Service}
		// A2：审核台上批准一条规格变更请求（checkpoint=spec-change）之后，规格升版
		// 并登记重排 v2。这是**唯一**能改 spec 生效的动作 —— agent 只能"提"。
		humanControlAPI.SpecChanges = specChangeApplier{
			specs:     spec.New(pipelinePool),
			discovery: discoveryService,
			pool:      pipelinePool,
		}
		// 重排 v2 的**派发意图**入口：人工打断判定"影响当前计划"之后，web 只登记
		// 意图（发现链第 6 步），派发与收产物由 coordinator 负责（同前五步的形状）。
		pipelineAPI.ReplanHook = func(ctx context.Context, planID, upstreamNodeID string, affected []string) error {
			return discoveryService.EnqueueReplan(ctx, planID, upstreamNodeID, affected)
		}
		// WithAgentTeams：设置页的"AgentTeams 选检"改为**真探** Controller（此前写死 false）。
		consoleAPI = web.Console{Service: console.New(pipelinePool).WithAgentTeams(atClient)}
	}
	// 仓库作用域的团队管理（2026-09-20 并入）：只有拿到库池才建服务；
	// 服务缺席时路由如实回 503 service_not_configured，不假装能用。
	agentTeamsAPI := web.AgentTeams{Client: atClient}
	if pipelinePool != nil {
		agentTeamsAPI.RepositoryTeams = repositoryteams.New(pipelinePool, atClient)
		// 建队时机 = **确认接入**:仓库页单仓「接入本项目」与批量「全部接入」
		// 走同一条路(见下方 OnRepositoriesConfirmed 与 spec §3.3)。
		teamService := agentTeamsAPI.RepositoryTeams
		// 房间号收敛是唯一的后台循环,与建队不冲突:它只**读**(对还没有房间号
		// 的行各发一个 GET),不建任何团队、不起任何 runtime —— 不补的话房间号
		// 恒空,"进房间看对话"永远是一间进不去的房。
		go func() {
			timer := time.NewTimer(20 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
				if filled, err := teamService.BackfillRooms(ctx); err != nil {
					slog.Warn("repository team room backfill deferred", "reason", err.Error())
				} else if filled > 0 {
					slog.Info("repository team rooms backfilled", "count", filled)
				}
				timer.Reset(5 * time.Minute)
			}
		}()
		// 建队时机:仓库**接入项目**即建(单仓/批量都建)——选仓门定稿
		// (2026-09-20,spec §3.3)取代此前"只有恰好新增 1 个仓才建"的裁定。
		//
		// 当天事故(一次批量接入灌出 42 队 / 124 worker、load 100+)不会因此
		// 重演,因为这次带着三道闸:
		//   · worker 一律建成 **Sleeping**(上游建 worker 原生带 state,一步休眠,
		//     不起真 runtime —— 每个 worker 可是一个真实实例);
		//   · EnsureForRepositories 逐仓**串行**、每仓间隔 2s,不给控制面制造尖峰;
		//   · 单仓失败只记 WARN,不阻断其余仓接入。
		// 唤醒(选进 issue 范围时)走 WakeTeamsForRepositories,fire-and-forget。
		projectAPI.OnRepositoriesConfirmed = func(ctx context.Context, projectID string, added []string) {
			for _, outcome := range teamService.EnsureForRepositories(ctx, projectID, added, repositoryTeamWorkerCount()) {
				switch {
				case outcome.Err != nil:
					slog.Warn("repository team ensure deferred",
						"project", projectID, "repository", outcome.RepositoryID, "reason", outcome.Err.Error())
				case outcome.Created:
					slog.Info("repository team created (sleeping)",
						"project", projectID, "repository", outcome.RepositoryID)
				}
			}
		}
	}
	observationModels, err := web.NewObservationModels(os.Getenv("REPOMESH_OBSERVE_WORKBENCH_URL"))
	if err != nil {
		fmt.Fprintln(stderr, "invalid local observation workbench address")
		return 1
	}
	modelAPI.Observation = observationModels
	if err := web.RunConfigured(ctx, *addr, *assets, auth, projectAPI, modelAPI, scanAPI, decisionAPI, skillsAPI, issuesAPI, messagesAPI, agentTeamsAPI, pipelineAPI, humanControlAPI, observeV1, discoveryAPI, consoleAPI, certFile, keyFile); err != nil {
		fmt.Fprintln(stderr, "web stopped:", err)
		return 1
	}
	return 0
}

// repositoryTeamWorkerCount 是"仓库接入时自动建队"给每支队伍配几名执行者。
//
// 默认 **1**（2026-09-20 线上实测后从 2 降下来）：AgentTeams 的每个 worker 都是一个
// 真实 runtime（embedded kube 里的一个 pod），而"接入即建队"面对的是几十个仓库 ——
// 每队 2 名 worker 时，9 支队伍就把这台机器压到 load 100+（实测 kube-apiserver /
// minio / 一堆 qwenpaw worker 一起抢 CPU），部署与验收全部开始超时。
// 一个仓库的**串行**交付本来也用不满第二名 worker；需要并行的项目用
// REPOMESH_REPOSITORY_TEAM_WORKERS 显式调大即可（越界或非数字一律退回默认）。
func repositoryTeamWorkerCount() int {
	if raw := strings.TrimSpace(os.Getenv("REPOMESH_REPOSITORY_TEAM_WORKERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 20 {
			return n
		}
	}
	return 1
}

// branchProvider 选数据库分支验证的 provider（评委①）：
//
//	· 配了 REPOMESH_POLAR_BRANCH_DSN → PolarDB（PostgreSQL 兼容集群，从**业务数据
//	  基线库**开分支；没有基线库就明确报错，不拿空库糊弄）；
//	· 否则 → 本地 PostgreSQL（同一实例 TEMPLATE 克隆）。
//
// 两条路径的 run 行里都记着 provider 名，证据不会互相冒充。
func branchProvider() branchvalidation.BranchProvider {
	if dsn := strings.TrimSpace(os.Getenv("REPOMESH_POLAR_BRANCH_DSN")); dsn != "" {
		return &branchvalidation.PolarProvider{
			BranchDSN:        dsn,
			BaselineDatabase: strings.TrimSpace(os.Getenv("REPOMESH_POLAR_BASELINE_DB")),
			ClusterID:        strings.TrimSpace(os.Getenv("REPOMESH_POLAR_CLUSTER_ID")),
		}
	}
	return &branchvalidation.LocalProvider{AdminDSN: os.Getenv("REPOMESH_DATABASE_URL")}
}

func runDatabase(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "Usage: repomesh-web db <check|migrate> [--database-url CONNECTION] [--timeout 30s]")
		fmt.Fprintln(stdout, "The connection defaults to REPOMESH_DATABASE_URL. Only migrate changes the schema.")
		return 0
	}
	if len(args) == 0 || args[0] != "check" && args[0] != "migrate" {
		fmt.Fprintln(stderr, "expected db check or db migrate; use db --help")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("repomesh-web db "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	databaseURL := flags.String("database-url", "", "PostgreSQL connection string (defaults to REPOMESH_DATABASE_URL)")
	timeout := flags.Duration("timeout", 30*time.Second, "total connection and operation timeout")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stdout, "Usage: repomesh-web db %s [options]\n", command)
			flags.SetOutput(stdout)
			flags.PrintDefaults()
			return 0
		}
		fmt.Fprintln(stderr, "invalid database command arguments; use db "+command+" --help")
		return 2
	}
	if flags.NArg() != 0 || *timeout <= 0 {
		fmt.Fprintln(stderr, "database commands require valid flags and a positive timeout")
		return 2
	}
	hasURLFlag := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "database-url" {
			hasURLFlag = true
		}
	})
	if !hasURLFlag {
		*databaseURL = os.Getenv("REPOMESH_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	db, err := database.Open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, database.ErrInvalidConfig) {
			return 2
		}
		return 1
	}
	defer db.Close()
	var state database.SchemaState
	if command == "check" {
		state, err = db.Check(ctx)
	} else {
		state, err = db.Migrate(ctx)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	status := "current"
	if state.Current == 0 {
		status = "missing"
	} else if state.Pending != 0 {
		status = "pending"
	}
	fmt.Fprintf(stdout, "schema status=%s current=%d target=%d pending=%d\n", status, state.Current, state.Target, state.Pending)
	if command == "check" && state.Pending != 0 {
		return 1
	}
	return 0
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func parsePlatformMap(value string) map[string]string {
	mapping := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		if host, platform, found := strings.Cut(strings.TrimSpace(pair), "="); found {
			mapping[strings.ToLower(host)] = strings.ToLower(platform)
		}
	}
	return mapping
}
