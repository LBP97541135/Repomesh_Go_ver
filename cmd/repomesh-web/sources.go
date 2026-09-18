package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/database"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/sources"
)

type sourceArguments struct {
	mode         string
	manifestPath string
	importID     string
}

func parseSourceArguments(arguments []string) (sourceArguments, error) {
	if len(arguments) == 0 {
		return sourceArguments{}, errors.New("expected sources import or sources result")
	}
	switch arguments[0] {
	case "import":
		flags := flag.NewFlagSet("sources import", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		path := flags.String("file", "", "manifest JSON file")
		if err := flags.Parse(arguments[1:]); err != nil || *path == "" || flags.NArg() != 0 {
			return sourceArguments{}, errors.New("usage: sources import --file PATH")
		}
		return sourceArguments{mode: "import", manifestPath: *path}, nil
	case "result":
		flags := flag.NewFlagSet("sources result", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		id := flags.String("import-id", "", "import UUID")
		if err := flags.Parse(arguments[1:]); err != nil || *id == "" || flags.NArg() != 0 {
			return sourceArguments{}, errors.New("usage: sources result --import-id UUID")
		}
		return sourceArguments{mode: "result", importID: strings.ToLower(*id)}, nil
	default:
		return sourceArguments{}, errors.New("expected sources import or sources result")
	}
}

func openSourceRuntime(ctx context.Context, databaseURL string) (*database.DB, *sources.Importer, error) {
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}
	state, err := db.Check(ctx)
	if err != nil || state.Pending != 0 {
		db.Close()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("schema is pending; run db migrate explicitly")
	}
	principal, err := sources.AuthenticateDeployment(ctx, db.Pool())
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	importer, err := sources.NewImporter(db.Pool(), principal, projects.NewCatalogWriter())
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, importer, nil
}

func runSourceImport(ctx context.Context, importer *sources.Importer, manifestPath string, output io.Writer) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil || len(data) > 1024*1024 {
		return errors.New("cannot read manifest")
	}
	command, err := sources.ParseImport(data)
	if err != nil {
		return err
	}
	result, err := importer.Import(ctx, command)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result.Receipt)
}

func runSourceResult(ctx context.Context, importer *sources.Importer, importID string, output io.Writer) error {
	receipt, err := importer.Get(ctx, importID)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(receipt)
}

func runSources(ctx context.Context, arguments []string, output, diagnostics io.Writer) int {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	parsed, err := parseSourceArguments(arguments)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	db, importer, err := openSourceRuntime(ctx, os.Getenv("REPOMESH_DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		if errors.Is(err, database.ErrInvalidConfig) {
			return 2
		}
		return 1
	}
	defer db.Close()
	switch parsed.mode {
	case "import":
		err = runSourceImport(ctx, importer, parsed.manifestPath, output)
	case "result":
		err = runSourceResult(ctx, importer, parsed.importID, output)
	}
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		var failure *sources.Failure
		if errors.As(err, &failure) && failure.Status == 2 {
			return 2
		}
		return 1
	}
	return 0
}
