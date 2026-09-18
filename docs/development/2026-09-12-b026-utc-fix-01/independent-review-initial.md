# B02.6 UTC independent review

Scope: read-only review of the B02.6 UTC change as present in the shared
working tree. No production, evidence, secret, database, or browser state was
modified.

## Verdict

No P0 or P1 finding. The proposed connection-level `timestamptz` codec is the
right boundary and preserves instants. One P2 test-isolation concern is below.

## Evidence and coverage

- `database.Open` registers `pgtype.TimestamptzCodec{ScanLocation: time.UTC}`
  in `pgxpool.Config.AfterConnect`. Therefore it is installed independently on
  every pool connection; it does not mutate PostgreSQL data or set the database
  session time zone.
- pgx v5.11.0's local `pgtype/timestamptz.go` documents that `ScanLocation`
  changes only the returned location, not the represented instant. Both binary
  and text scan plans call `tim.In(plan.location)` when it is non-nil. Encoding
  remains the normal timestamptz instant encoding.
- The only product pool construction is `database.Open`; authenticated runtime
  construction passes its pool to access/secrets. `internal/testdb` does the
  same. This covers database-originated Session connection `observedAt`, Start
  and replay `expiresAt`, Attempt `observedAt`, repository/discovery coverage
  and item observation times, and internal comparison values. Provider-created
  times are not database scans and retain their existing semantics.
- Browser `web/src/api.ts` accepts timestamp strings only with a trailing `Z`.
  The codec produces UTC `time.Time` values, so `encoding/json` emits a `Z`
  representation (including the observed microseconds); NULL remains NULL.
- The new test obtains two connections, exercises cached/binary and exec/text
  results, checks the original instant, UTC Location, JSON `Z`, and NULL.
  pgx v5.11.0 documents `QueryExecModeExec` as text-formatted parameters and
  results, so the labels are accurate.

## Findings

### P2 — test changes process-global `time.Local`

`TestPostgresTimestamptzScansInUTCOnEveryConnection` assigns `time.Local` and
restores it with cleanup. Current database tests do not call `t.Parallel`, so
there is no demonstrated current race. Still, `time.Local` is process-global;
future parallel tests or background work in this package could observe the
temporary Los Angeles location. Prefer a subprocess with `TZ=America/Los_Angeles`
for this integration assertion, or document/serialize this intentional global
test state under a package-level test mutex. This is test hygiene, not a product
correctness failure.

## Verification performed

My local `REPOMESH_TEST_DATABASE_URL` was unset. `go test ./internal/database
-run '^TestPostgresTimestamptzScansInUTCOnEveryConnection$' -count=1` compiled
and passed by taking the existing integration-test skip path; no database or
browser was contacted.

I also read the separate r2 evidence summary at
`/tmp/repomesh-b026-utc-fix-01`: its isolated PostgreSQL run records the
expected pre-fix failure (all four cases retained the instant but had the Los
Angeles Location) and the post-fix pass for both pool connections, binary/text,
NULL, and JSON `Z`. Its existing full `go test ./...` record is not an overall
pass: `TestPostgresInvalidCredentialsAreRedacted` fails under the peer/trust
socket because PostgreSQL ignores the deliberately wrong password. That is a
test-environment authentication issue outside the UTC change; a private SCRAM
cluster rerun is required before treating the complete suite as passing.
