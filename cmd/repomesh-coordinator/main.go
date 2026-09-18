package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/buildinfo"
	"repomesh.local/repomesh/internal/database"
	"repomesh.local/repomesh/internal/decisionchain"
	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/tasks"
)

// protocolVersion is the send-permit protocol this coordinator binary speaks.
// It must stay within the 64-character limit enforced by
// models.NewSingleRequestTransport, and it is checked against the test rows
// themselves, so a protocol change requires re-registering handlers.
const protocolVersion = "repomesh-model-test-v1"

func main() { os.Exit(run()) }

func run() int {
	args := os.Args[1:]
	if len(args) >= 1 && args[0] == "unknown" {
		return runUnknownMaintenance(args[1:])
	}
	return runWorker(args)
}

func runWorker(args []string) int {
	flags := flag.NewFlagSet("repomesh-coordinator", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	version := flags.Bool("version", false, "print release version and exit")
	config := flags.String("auth-config", os.Getenv("REPOMESH_AUTH_CONFIG"), "authentication deployment JSON file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Printf("repomesh-coordinator %s\n", buildinfo.Version)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		return 2
	}
	if *config == "" {
		fmt.Fprintln(os.Stderr, "repomesh-coordinator: authentication is not configured; no background work is running")
		return 1
	}
	// C2 fix: dispatch must register attempts under the SAME worker id the
	// host executor registers with, or the executor's claim JOIN never
	// matches. The default matches the deployed repomesh-host-executor unit.
	dispatchWorker := strings.TrimSpace(os.Getenv("REPOMESH_EXECUTOR_WORKER"))
	if dispatchWorker == "" {
		dispatchWorker = "wrk_accept_codex"
	}
	// C4 fix: the dispatched agent kind is a deployment decision, not a
	// hardcoded literal; codex_cli and claude_cli are both valid.
	agentKind := strings.TrimSpace(os.Getenv("REPOMESH_DISPATCH_AGENT_KIND"))
	if agentKind == "" {
		agentKind = "codex_cli"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	runtime, err := access.OpenRuntime(startup, *config, os.Getenv("REPOMESH_DATABASE_URL"))
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "coordinator startup:", err)
		return 1
	}
	defer runtime.Close()

	// B05: model test dispatch runs beside the access worker. The budget
	// store is created fresh here, mirroring the web process — windows
	// initialize lazily, so an empty ledger only means zero-quota unknown.
	budgets := modelbudget.New()
	testService := models.NewTestService(runtime.Pool(), runtime.Service, runtime.SecretStore(), budgets)
	// 自动托管 engine: the coordinator, not the browser, advances the
	// discovery state machine (analysis → candidates → classification →
	// auto approval → plan → materialize). Decision-chain audit wiring
	// mirrors the web composition root.
	discoveryService := discovery.New(runtime.Pool()).WithDecisions(decisionchain.New(decisionchain.Config{
		EmbeddingBaseURL: os.Getenv("REPOMESH_EMBEDDING_BASE_URL"),
		EmbeddingAPIKey:  os.Getenv("REPOMESH_EMBEDDING_API_KEY"),
		EmbeddingModel:   os.Getenv("REPOMESH_EMBEDDING_MODEL"),
	}, runtime.Pool()))
	automator := newDiscoveryAutomator(discoveryService, runtime.Pool())
	transport, err := models.NewSingleRequestTransport(protocolVersion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "model test transport:", err)
		return 1
	}
	runner := models.NewTestRunner(testService, transport)
	if err := runner.RegisterHandler(ctx, protocolVersion); err != nil {
		fmt.Fprintln(os.Stderr, "model test handler registration:", err)
		return 1
	}
	taskStore := tasks.NewPostgresStore(runtime.Pool())
	dagLedger := &coordinatorLedger{pool: runtime.Pool()}
	// Delivery pipeline loop: independent of the auth worker so a hung
	// upstream call inside RunOne cannot stall 自动托管 and DAG dispatch.
	// SKIP LOCKED keeps this loop safe against any future concurrent scheduler.
	go func() {
		for ctx.Err() == nil {
			slog.Info("pipeline: tick")
			dagCtx, dagCancel := context.WithTimeout(ctx, 10*time.Second)
			automator.step(dagCtx)
			if _, promoteErr := taskStore.PromoteReady(dagCtx); promoteErr != nil {
				slog.Warn("dag promotion deferred", "reason", promoteErr.Error())
			}
			if _, dispatchErr := taskStore.DispatchOne(dagCtx, dagLedger, dispatchWorker, agentKind); dispatchErr != nil {
				slog.Warn("dag dispatch deferred", "reason", dispatchErr.Error())
			}
			dagCancel()
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	lastHeartbeat := time.Now()
	slog.Info("authentication coordinator started", "version", buildinfo.Version)
	for ctx.Err() == nil {
		heartbeat := time.Since(lastHeartbeat) >= 10*time.Second
		if heartbeat {
			hbCtx, hbCancel := context.WithTimeout(ctx, 5*time.Second)
			err := runner.HeartbeatHandler(hbCtx)
			hbCancel()
			if err != nil && ctx.Err() != nil {
				break
			}
			if err != nil {
				var leaseLost *models.Failure
				if errors.As(err, &leaseLost) && leaseLost.Code == "TEST_HANDLER_LEASE_LOST" {
					if regErr := runner.RegisterHandler(ctx, protocolVersion); regErr != nil {
						slog.Warn("model test handler re-registration failed", "reason", regErr.Error())
					} else {
						slog.Info("model test handler lease re-registered")
					}
				} else if ctx.Err() == nil {
					slog.Warn("model test heartbeat deferred", "reason", err.Error())
				}
			}
			lastHeartbeat = time.Now()
		}
		work, cancel := context.WithTimeout(ctx, 15*time.Second)
		worked, err := runtime.Service.RunOne(work)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Warn("authentication work deferred", "reason", err.Error())
		}
		if !worked && err == nil {
			testCtx, testCancel := context.WithTimeout(ctx, 30*time.Second)
			testWorked, testErr := runner.RunOne(testCtx)
			testCancel()
			if testErr != nil && ctx.Err() == nil {
				slog.Warn("model test dispatch deferred", "reason", testErr.Error())
			}
			worked = testWorked
		}
		delay := time.Second
		if worked && err == nil {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return 0
}

// runUnknownMaintenance provides the operator CLI for tests left in the
// unknown state: inspect reads the immutable bindings, close finalizes them
// after the operator supplies digest-bound evidence on stdin.
func runUnknownMaintenance(args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: repomesh-coordinator unknown <inspect|close> ACTOR_ID TEST_ID [CLOSE_KEY]")
		fmt.Println("close reads the canonical evidence JSON from stdin.")
		return 0
	}
	if len(args) < 3 || args[0] != "inspect" && args[0] != "close" {
		fmt.Fprintln(os.Stderr, "expected unknown inspect or unknown close; use unknown --help")
		return 2
	}
	command, actorID, testID := args[0], args[1], args[2]
	if command == "close" && len(args) != 4 {
		fmt.Fprintln(os.Stderr, "unknown close requires ACTOR_ID TEST_ID CLOSE_KEY")
		return 2
	}
	if command == "inspect" && len(args) != 3 {
		fmt.Fprintln(os.Stderr, "unknown inspect requires ACTOR_ID TEST_ID")
		return 2
	}
	flags := flag.NewFlagSet("repomesh-coordinator unknown "+command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databaseURL := flags.String("database-url", os.Getenv("REPOMESH_DATABASE_URL"), "PostgreSQL connection string")
	if err := flags.Parse(args[3:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *databaseURL == "" {
		fmt.Fprintln(os.Stderr, "unknown commands require a database URL (flag or REPOMESH_DATABASE_URL)")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database open:", err)
		return 1
	}
	defer db.Close()
	principal, err := models.AuthenticateUnknownMaintenance(ctx, db.Pool())
	if err != nil {
		fmt.Fprintln(os.Stderr, "maintenance authentication:", err)
		return 1
	}
	maintenance, err := models.NewUnknownMaintenance(db.Pool(), principal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "maintenance startup:", err)
		return 1
	}
	switch command {
	case "inspect":
		inspection, err := maintenance.InspectUnknownTest(ctx, actorID, testID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "inspect failed:", err)
			return 1
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(inspection); err != nil {
			fmt.Fprintln(os.Stderr, "inspect output:", err)
			return 1
		}
	case "close":
		evidence, err := io.ReadAll(io.LimitReader(os.Stdin, 256*1024+1))
		if err != nil || len(evidence) > 256*1024 {
			fmt.Fprintln(os.Stderr, "evidence unreadable or too large")
			return 2
		}
		commands, err := models.ParseUnknownCloseCommand(actorID, testID, strings.TrimSpace(args[3]), evidence)
		if err != nil {
			fmt.Fprintln(os.Stderr, "evidence rejected:", err)
			return 2
		}
		if err := maintenance.ValidateUnknownClose(ctx, commands); err != nil {
			fmt.Fprintln(os.Stderr, "close validation failed:", err)
			return 1
		}
		receipt, err := maintenance.CloseUnknownTest(ctx, commands)
		if err != nil {
			fmt.Fprintln(os.Stderr, "close failed:", err)
			return 1
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(receipt); err != nil {
			fmt.Fprintln(os.Stderr, "close output:", err)
			return 1
		}
	}
	return 0
}
