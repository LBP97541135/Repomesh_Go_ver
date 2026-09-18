# B02.6 external-preparation independent read-only review

Review time: 2026-09-12.

## Verdict

PASS after one documentation-status correction. B02.6 remains BLOCKED. B02 remains IN_PROGRESS. B03 remains TODO. The documents do not claim that a real GitHub App, OAuth browser round trip, Nginx deployment, or natural token refresh passed.

## Issue found and resolved during review

`docs/development/2026-09-12-b02-external-preparation/PLAN.md` originally left steps 15 and 17 through 20 unchecked even though the README and decision log said that the audit, guide, template, and handoff update were complete. The integrating agent corrected the checklist before this verdict. Lines 15 through 18 and 20 now show completed work. Line 19 remains unchecked for this independent review. Lines 21 and 22 remain unchecked for real B02.6 execution and B03 entry.

## Source and document fact check

- The App guidance matches `internal/github/app.go:39-74`. The capability check calls the installation endpoint and requires Metadata read, Contents write, and Pull requests write. The denied reason codes named by the guide are implemented.
- The HTTP routes and results match `internal/web/auth.go:47-130`. The guide uses the callback route, session route, repository route, 303 callback redirect, `AUTH_NOT_CONFIGURED`, and the cookie constraints consistently.
- The SQL snapshot in `docs/current/b02-github-live-acceptance.md:184-197` names columns that exist in `internal/database/migrations/0003_authentication.sql:18-42`.
- The refresh threshold is implemented in `internal/access/worker.go:45-48` as 30 seconds before access-token expiry. The session idle limit is 30 minutes in `internal/access/session.go:16-21`. The external guide correctly retains the 12-hour session-cookie absolute lifetime and the 60-second repository observation rule. The revised loss-of-read-access instruction at guide line 170 and template line 34 waits past that cache interval.
- The configuration fields and commands match `configs/auth.example.json`, `cmd/repomesh-web/main.go:34-69`, and `cmd/repomesh-coordinator/main.go:19-64`. The guide keeps secrets out of examples, uses protected files, and uses the final r1 package without overwriting historical evidence.
- The GitHub token lifetime statement was checked against the linked official GitHub page. It states eight hours for user access tokens and six months for refresh tokens when expiration is enabled.
- The Nginx content is a configuration review only. No Nginx installation, `nginx -t`, reload, service start, product test, historic test, or external GitHub operation was performed or reported as performed.

## Evidence and preservation checks

- Recomputed SHA-256 values for all 70 files recorded in `batch-02-baseline.json`. No mismatch.
- Recomputed SHA-256 values for all 69 files in `docs/development/2026-09-12-batch-02/final-source.json`. No mismatch.
- Recomputed all 11 hashes in `dist/repomesh-0.2.0-b02-local-20260912-r1/release.json`. No mismatch.
- `.codex/config.toml` has the workspace-baseline hash `58c3d47686d46de2074c7acc7bb897d41807b4ed89fad8d20482448048e5cbad`. Its Git modification existed before this preparation task.
- The workspace baseline differs only at the four authorized current documents: `README.md`, `docs/current/HANDOFF.md`, `docs/current/IMPLEMENTATION-PLAN.md`, and `docs/current/authentication-development.md`.
- Local Markdown links checked in README, `docs/current`, and the external-preparation directory resolved. The external-preparation bash snippets passed `bash -n`. `git diff --check` reported no whitespace error.

## Verification boundary

This is a read-only review of source, documents, hashes, and syntax. It is separate from the prior local product and browser evidence. I did not rerun those checks. I did not read secrets or a complete prior session transcript. The report does not add a real-acceptance PASS claim.
