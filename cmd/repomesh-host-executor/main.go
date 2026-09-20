package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"repomesh.local/repomesh/internal/buildinfo"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "typesafe" {
		return runTypeSafeHelper(ctx, args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("repomesh-host-executor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.Bool("version", false, "print release version and exit")
	databaseURL := flags.String("database", os.Getenv("REPOMESH_DATABASE_URL"), "PostgreSQL connection string")
	workerID := flags.String("worker", os.Getenv("REPOMESH_EXECUTOR_WORKER"), "registered worker id")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Fprintf(stdout, "repomesh-host-executor %s\n", buildinfo.Version)
		return 0
	}
	if *databaseURL == "" || *workerID == "" {
		fmt.Fprintln(stderr, "repomesh-host-executor: requires --database and --worker (no unregistered host actions)")
		return 1
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, err := openExecution(ctx, *databaseURL, *workerID)
	if err != nil {
		fmt.Fprintln(stderr, "host-executor startup:", err)
		return 1
	}
	defer service.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			if err := service.RunOne(ctx); err != nil {
				fmt.Fprintln(stderr, "host-executor step:", err)
			}
		}
	}
}
