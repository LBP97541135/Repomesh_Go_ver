package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

func TestCLITrialVerdictsDatasetAndNoOverwrite(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	for _, tc := range []struct {
		variant string
		exit    int
	}{{"baseline", 1}, {"candidate", 0}, {"assembly-mismatch", 2}} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), []string{"discount", "--archive", archive, "--variant", tc.variant}, &out, &stderr); code != tc.exit {
			t.Fatalf("%s exit=%d: %s", tc.variant, code, stderr.String())
		}
		if out.Len() == 0 {
			t.Fatal("result missing")
		}
	}
	path := filepath.Join(t.TempDir(), "dataset.csv")
	var out, stderr bytes.Buffer
	args := []string{"dataset", "--archive", archive, "--output", path}
	if code := run(t.Context(), args, &out, &stderr); code != 0 {
		t.Fatalf("dataset: %d %s", code, stderr.String())
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil || len(rows) != 4 {
		t.Fatal(len(rows), err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := run(t.Context(), args, &out, &stderr); code != 2 {
		t.Fatal("overwrote previous dataset")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing data changed")
	}
}

func TestCLIRejectsUnknownAndUnconfiguredInput(t *testing.T) {
	for _, args := range [][]string{{"not-a-command"}, {"collect", "--archive", filepath.Join(t.TempDir(), "archive")}, {"discount", "--archive", filepath.Join(t.TempDir(), "archive")}} {
		var out, stderr bytes.Buffer
		if code := run(t.Context(), args, &out, &stderr); code != 2 {
			t.Fatal("invalid request accepted", args)
		}
	}
}
