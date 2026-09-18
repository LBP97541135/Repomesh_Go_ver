package database

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresTimestamptzScansInUTCOnEveryConnection(t *testing.T) {
	const childMarker = "REPOMESH_UTC_TEST_CHILD"
	if os.Getenv(childMarker) == "" {
		command := exec.Command(os.Args[0], "-test.run=^TestPostgresTimestamptzScansInUTCOnEveryConnection$", "-test.v")
		command.Env = append(os.Environ(), childMarker+"=1", "TZ=America/Los_Angeles")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("UTC test subprocess failed: %v\n%s", err, output)
		}
		return
	}

	db := openTestDatabase(t, isolatedDatabase(t))
	first, err := db.pool.Acquire(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := db.pool.Acquire(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()

	expected := time.Date(2026, time.September, 12, 13, 58, 16, 237684000, time.UTC)
	for connectionIndex, connection := range []*pgx.Conn{first.Conn(), second.Conn()} {
		for _, queryMode := range []struct {
			name string
			mode pgx.QueryExecMode
		}{
			{name: "binary", mode: pgx.QueryExecModeCacheStatement},
			{name: "text", mode: pgx.QueryExecModeExec},
		} {
			t.Run(queryMode.name+"/connection-"+string(rune('1'+connectionIndex)), func(t *testing.T) {
				var observed time.Time
				var nullable pgtype.Timestamptz
				err := connection.QueryRow(
					testContext(t),
					"SELECT '2026-09-12 06:58:16.237684-07'::timestamptz, NULL::timestamptz",
					queryMode.mode,
				).Scan(&observed, &nullable)
				if err != nil {
					t.Fatal(err)
				}
				if !observed.Equal(expected) {
					t.Fatalf("instant=%s; want %s", observed, expected)
				}
				if observed.Location() != time.UTC {
					t.Fatalf("location=%s; want UTC", observed.Location())
				}
				encoded, err := json.Marshal(observed)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := string(encoded), `"2026-09-12T13:58:16.237684Z"`; got != want {
					t.Fatalf("JSON=%s; want %s", got, want)
				}
				if nullable.Valid {
					t.Fatal("NULL timestamptz became valid")
				}
				if encodedNull, err := json.Marshal(nullable); err != nil || string(encodedNull) != "null" {
					t.Fatalf("NULL JSON=%s error=%v; want null", encodedNull, err)
				}
			})
		}
	}
}
