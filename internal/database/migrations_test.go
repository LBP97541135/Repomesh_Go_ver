package database

import (
	"io/fs"
	"sort"
	"strconv"
	"testing"
)

// TestEmbeddedMigrationManifest guards the migration set that actually ships.
//
// TestMigrationManifest only feeds loadMigrations synthetic fstest.MapFS inputs
// (including "duplicate version"), so it never looks at the real embedded
// manifest. That left the one failure mode that takes production down
// unguarded: two files sharing a version prefix.
//
// 2026-09-20: two concurrent branches each added a 0053_*.sql. Nothing failed at
// compile time. loadMigrations only verifies contiguity at run time, so
// `repomesh-web db migrate` died at parse time (before touching the database)
// and the deploy step exited non-zero.
func TestEmbeddedMigrationManifest(t *testing.T) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	byVersion := map[int64][]string{}
	for _, entry := range entries {
		match := migrationFilename.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			t.Fatalf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			t.Fatalf("invalid migration version in %q: %v", entry.Name(), err)
		}
		byVersion[version] = append(byVersion[version], entry.Name())
	}
	if len(byVersion) == 0 {
		t.Fatal("embedded migration manifest is empty")
	}

	versions := make([]int64, 0, len(byVersion))
	for version := range byVersion {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	for index, version := range versions {
		names := byVersion[version]
		if len(names) > 1 {
			t.Fatalf(
				"migration version %04d is claimed by %d files %v: "+
					"a shared version prefix makes loadMigrations fail at parse time, "+
					"so `db migrate` dies before it reaches the database",
				version, len(names), names)
		}
		if version != int64(index+1) {
			t.Fatalf("migration versions must be contiguous from one: expected %04d, found %04d (%v)",
				index+1, version, names)
		}
	}

	manifest, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("embedded migration manifest is rejected by loadMigrations: %v", err)
	}
	if len(manifest) != len(versions) {
		t.Fatalf("loadMigrations returned %d migrations for %d files on disk", len(manifest), len(versions))
	}
	if highest := manifest[len(manifest)-1].version; highest != int64(len(versions)) {
		t.Fatalf("highest migration is %04d but the manifest holds %d files", highest, len(versions))
	}
	t.Logf("embedded migrations are contiguous: 0001..%04d (%d files)", len(versions), len(versions))
}
