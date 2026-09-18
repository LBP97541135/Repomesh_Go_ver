# B02.6 UTC independent closure review

Scope: read-only closure review of the final UTC regression-test isolation and
the supplied validation records. No checks were rerun and no database, browser,
secret, or runtime configuration was accessed.

## Finding status

- P0: none.
- P1: none.
- P2: none. The prior P2 is closed.

## P2 closure

`internal/database/utc_test.go` no longer assigns `time.Local`. The parent test
executes its own test binary with an explicit child marker and
`TZ=America/Los_Angeles`; only that child opens the isolated test database and
runs the two-connection binary/text/NULL/JSON assertions. This maintains the
non-UTC regression condition without process-global mutable time state in the
parent test process, so it removes the prior future-parallel-test coupling.

The child marker prevents recursion. `exec.Command(os.Args[0],
"-test.run=^TestPostgresTimestamptzScansInUTCOnEveryConnection$", "-test.v")`
uses the existing test executable, and its environment inherits the test
database configuration while the appended TZ selects the intended local zone.

## Validation-record conclusion

`/tmp/repomesh-b026-utc-fix-01/source-scope.txt` records that the UTC fix owns
only the `database.Open` codec registration and the new regression test; it
also identifies `DB.Pool` as pre-existing uncommitted B02 work. The production
diff remains the reviewed r2 codec registration.

`/tmp/repomesh-b026-utc-validation-02/summary.txt` and `go-test.json` record a
completed isolated PostgreSQL 17 SCRAM run: `go test -count=1 -json ./...`
exited 0 with 230 runs, 229 passes, zero failures, and one permitted
privilege-dependent skip (`internal/secrets TestRootFileOwner`). No PostgreSQL
integration test skipped. The JSON log includes passing UTC subtests for both
connections and both binary/text modes, and passing
`TestPostgresInvalidCredentialsAreRedacted`. The temporary cluster was stopped
and its generated data/password artifacts trashed; port 55433 no longer
listens.
