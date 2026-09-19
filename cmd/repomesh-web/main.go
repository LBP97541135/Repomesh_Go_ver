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
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/branchvalidation"
	"repomesh.local/repomesh/internal/buildinfo"
	"repomesh.local/repomesh/internal/console"
	"repomesh.local/repomesh/internal/database"
	"repomesh.local/repomesh/internal/decisionchain"
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
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/reposcan"
	"repomesh.local/repomesh/internal/scan"
	"repomesh.local/repomesh/internal/scm"
	skills "repomesh.local/repomesh/internal/skills"
	"repomesh.local/repomesh/internal/spec"
	"repomesh.local/repomesh/internal/tasks"
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
		modelAPI = web.Models{Service: modelService, Tests: testService, Applications: applyService}
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
		pipelineAPI.HandoffDocs = web.HandoffDocs{Service: handoff.New(runtime.Pool())}
		pipelineAPI.Extensions = web.PipelineExtensions{
			Spec:  spec.New(runtime.Pool()),
			Gates: gates.New(runtime.Pool()),
		}
		pipelineAPI.JointValidation = web.JointValidation{Service: jointvalidation.New(runtime.Pool())}
		pipelineAPI.SCMRoutes = web.PipelineSCM{
			SCM:         scm.New(runtime.Pool(), os.Getenv("REPOMESH_WEBHOOK_SECRET")),
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
	}		// Same guard convention as the decision block: writes require Origin +
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
		scanAPI = web.Scan{API: &scan.HTTP{
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
		}}
	}
	// M1-M9 pipeline assembly (own pool; works with or without auth-config).
	if pipelinePool != nil {
		pipelineAPI = web.Pipeline{
			Tasks:        tasks.NewPostgresStore(pipelinePool),
			Assembly:     assembly.New(pipelinePool, atClient, nil),
			InterfaceDoc: interfacedoc.New(pipelinePool),
			BranchValid:  branchvalidation.New(pipelinePool, &branchvalidation.LocalProvider{AdminDSN: os.Getenv("REPOMESH_DATABASE_URL")}),
			Observation:  observability.New(pipelinePool),
			Extensions: web.PipelineExtensions{
				Spec:  spec.New(pipelinePool),
				Gates: gates.New(pipelinePool),
			},
			JointValidation: web.JointValidation{Service: jointvalidation.New(pipelinePool)},
			SCMRoutes: web.PipelineSCM{
				SCM:         scm.New(pipelinePool, os.Getenv("REPOMESH_WEBHOOK_SECRET")),
				Observation: observability.New(pipelinePool),
			},
			HandoffDocs: web.HandoffDocs{Service: handoff.New(pipelinePool)},
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
		}, pipelinePool))
		discoveryAPI = web.Discovery{Service: discoveryService, Maintenance: discovery.NewMaintenance(pipelinePool)}
		consoleAPI = web.Console{Service: console.New(pipelinePool)}
	}
	if err := web.RunConfigured(ctx, *addr, *assets, auth, projectAPI, modelAPI, scanAPI, decisionAPI, skillsAPI, issuesAPI, messagesAPI, web.AgentTeams{Client: atClient}, pipelineAPI, humanControlAPI, observeV1, discoveryAPI, consoleAPI, certFile, keyFile); err != nil {
		fmt.Fprintln(stderr, "web stopped:", err)
		return 1
	}
	return 0
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
