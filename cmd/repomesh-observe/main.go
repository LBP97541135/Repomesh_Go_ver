// repomesh-observe collects committed facts and runs independent evaluations.
// It never starts product workers or applies product database migrations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/observepipe"
	"repomesh.local/repomesh/internal/observeui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `Usage: repomesh-observe <command> [flags]
  serve            --archive DIR [--read-archive DIR ...] [--addr 127.0.0.1:18090]
                   local workbench, OTLP, Jev rubric, DeepSeek analysis and human labels
  collect          --archive DIR --project ID --issue ID --source-id NAME
                   reads REPOMESH_OBSERVE_DATABASE_URL (no migration or writes)
  discount         --archive DIR --variant baseline|candidate|assembly-mismatch
                   or --manifest FILE for explicitly configured HTTP services
  export           --archive DIR --config configs/agentloop.example.json
  dataset          --archive DIR --output FILE.csv
  import-result    --archive DIR --raw FILE --binding FILE --grader FILE
  status           --archive DIR
  doctor           --config configs/agentloop.example.json
Exit: 0 command succeeded / trial pass; 1 known trial fail; 2 error or unknown.
OTLP acknowledgment is transport only; cloud indexing/evaluation needs separate evidence.
`

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, usage)
		return 0
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(errOut)
	archive := flags.String("archive", "", "private evidence directory")
	addr := flags.String("addr", "127.0.0.1:18090", "local workbench loopback address")
	var readArchives stringList
	flags.Var(&readArchives, "read-archive", "existing read-only archive (repeatable)")
	config := flags.String("config", "configs/agentloop.example.json", "export configuration (references env secrets)")
	project := flags.String("project", "", "exact project identity")
	issue := flags.String("issue", "", "exact issue identity")
	sourceID := flags.String("source-id", "", "stable non-secret source database/deployment identity")
	variant := flags.String("variant", "", "fixed fixture variant")
	manifest := flags.String("manifest", "", "frozen discount HTTP manifest")
	output := flags.String("output", "", "new CSV output path")
	rawPath := flags.String("raw", "", "platform result JSON")
	bindingPath := flags.String("binding", "", "trusted platform/local identity mapping")
	graderPath := flags.String("grader", "", "fixed grader configuration")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	printJSON := func(value any) { _ = json.NewEncoder(out).Encode(value) }
	fail := func(err error) int { fmt.Fprintln(errOut, err); return 2 }
	if args[0] == "serve" {
		if err := observeui.ListenAddress(*addr); err != nil {
			return fail(err)
		}
		server, err := observeui.New(observeui.Options{Archive: *archive, ReadArchives: readArchives})
		if err != nil {
			return fail(err)
		}
		if err = observeui.Serve(ctx, *addr, server, out); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "doctor" {
		c, err := observepipe.LoadExportConfig(*config)
		if err != nil {
			return fail(err)
		}
		printJSON(map[string]any{"destination_id": c.DestinationID, "endpoint_configured": strings.TrimSpace(os.Getenv(c.EndpointEnv)) != "", "service_name": c.ServiceName, "cloud_agentloop": "not_verified", "dsh": "not_connected", "collector_build": observepipe.CollectorBuild()})
		return 0
	}
	if !map[string]bool{"collect": true, "discount": true, "export": true, "dataset": true, "import-result": true, "status": true}[args[0]] {
		fmt.Fprint(errOut, usage)
		return 2
	}
	j, err := observepipe.OpenJournal(*archive)
	if err != nil {
		return fail(err)
	}
	switch args[0] {
	case "collect":
		if *sourceID == "" || *project == "" || *issue == "" {
			return fail(errors.New("collect requires source-id, project and issue"))
		}
		dsn := os.Getenv("REPOMESH_OBSERVE_DATABASE_URL")
		if dsn == "" {
			return fail(errors.New("REPOMESH_OBSERVE_DATABASE_URL is not configured"))
		}
		collectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		pool, err := pgxpool.New(collectCtx, dsn)
		if err != nil {
			return fail(errors.New("invalid observation database configuration"))
		}
		defer pool.Close()
		if err = pool.Ping(collectCtx); err != nil {
			return fail(errors.New("cannot connect observation database"))
		}
		summary, err := observepipe.CollectDiscoveryFrom(collectCtx, observability.New(pool), j, *sourceID, *project, *issue)
		if err != nil {
			return fail(err)
		}
		printJSON(summary)
		return 0
	case "discount":
		if (*variant == "") == (*manifest == "") {
			return fail(errors.New("provide exactly one of variant or manifest"))
		}
		var r observepipe.TrialResult
		if *manifest != "" {
			var m observepipe.DiscountManifest
			if err = loadJSON(*manifest, &m); err != nil {
				return fail(err)
			}
			r, err = observepipe.VerifyDiscount(ctx, m)
		} else {
			r, err = observepipe.RunDiscountFixture(ctx, *variant)
		}
		if err != nil {
			return fail(err)
		}
		a, err := observepipe.ArchiveTrial(j, r)
		if err != nil {
			return fail(err)
		}
		printJSON(a)
		if r.Verdict == "pass" {
			return 0
		}
		if r.Verdict == "fail" {
			return 1
		}
		return 2
	case "export":
		c, err := observepipe.LoadExportConfig(*config)
		if err != nil {
			return fail(err)
		}
		summary, err := observepipe.ExportOTLP(ctx, j, c)
		printJSON(summary)
		if err != nil {
			return fail(err)
		}
		return 0
	case "dataset":
		rows, err := observepipe.DatasetRows(j)
		if err != nil {
			return fail(err)
		}
		if len(rows) == 0 {
			return fail(errors.New("no completed archived trials to export"))
		}
		var b bytes.Buffer
		if err = observepipe.WriteTrialCSV(&b, rows); err != nil {
			return fail(err)
		}
		if err = writeNew(*output, b.Bytes()); err != nil {
			return fail(err)
		}
		printJSON(map[string]any{"rows": len(rows), "output": *output, "platform_import": "not_performed"})
		return 0
	case "import-result":
		var binding observepipe.PlatformBinding
		var grader observepipe.GraderConfig
		if err = loadJSON(*bindingPath, &binding); err != nil {
			return fail(err)
		}
		if err = loadJSON(*graderPath, &grader); err != nil {
			return fail(err)
		}
		var a observepipe.ArchivedTrial
		if err = j.ReadRecord("trials", binding.TrialID, &a); err != nil {
			return fail(errors.New("binding must reference an archived trial"))
		}
		if binding.DataScope == "trace" {
			found := false
			for _, id := range a.TraceIDs {
				if id == binding.SubjectTraceID {
					found = true
					break
				}
			}
			if !found {
				return fail(errors.New("bound subject trace does not belong to the archived trial"))
			}
		}
		var raw json.RawMessage
		if err = loadJSON(*rawPath, &raw); err != nil {
			return fail(err)
		}
		rawRef, err := j.PutJSON(raw)
		if err != nil {
			return fail(err)
		}
		r, err := observepipe.NormalizePlatformResult(raw, binding, grader)
		if err != nil {
			return fail(err)
		}
		if err = j.PutRecord("platform", r.ResultKey(), r); err != nil {
			return fail(err)
		}
		printJSON(map[string]any{"trial_id": r.TrialID, "status": r.Status, "verdict": r.Verdict, "raw_ref": rawRef, "deterministic_verdict": a.Verdict, "note": "platform grade is retained separately; it does not overwrite deterministic acceptance"})
		return 0
	case "status":
		events, err := j.Events()
		if err != nil {
			return fail(err)
		}
		rows, err := observepipe.DatasetRows(j)
		if err != nil {
			return fail(err)
		}
		counts := map[string]int{"pass": 0, "fail": 0, "unknown": 0}
		for _, r := range rows {
			counts[r.Verdict]++
		}
		printJSON(map[string]any{"events": len(events), "trials": len(rows), "outcomes": counts, "scope": "local_evidence_pipeline", "cloud_agentloop": "not_verified", "dsh": "not_connected"})
		return 0
	}
	return 2
}

type stringList []string

func (s *stringList) String() string         { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error { *s = append(*s, value); return nil }

func loadJSON(path string, dest any) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("cannot read input JSON file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil || len(b) > 32<<20 {
		return errors.New("input JSON exceeds limit or is unreadable")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(dest) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid input JSON or unsupported fields")
	}
	return nil
}

func writeNew(path string, data []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".dataset-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return errors.New("cannot publish dataset (existing outputs are never replaced)")
	}
	return nil
}
