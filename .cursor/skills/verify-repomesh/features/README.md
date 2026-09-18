# RepoMesh verification map

This directory is the maintained source for verifying user-facing RepoMesh behavior. Read this index before driving the app. Use the matching feature file as the recipe.

## Baseline preconditions

- Skill launch, doctor, and drive use a local origin from operator yaml, default `http://127.0.0.1:18080`. Copy `config.example.yaml` to `config.yaml` before filling live fields. Human steps are in [OPERATOR-GUIDE.md](../OPERATOR-GUIDE.md). If live Web already owns 18080, unconfigured proof uses another port.
- Unconfigured UI proofs use that isolated Web started by `helpers/launch.sh` with `run_scope: unconfigured-only`.
- Live GitHub proofs need `run_scope: account-a-live` (or `restore-leftovers`), `auth.json` with a product HTTPS origin, and a coordinator you started. Local HTTP is still the helper listen address. It does not prove `__Host-` cookies. Proven loopback cookie origin on the Cloud VM is `https://127.0.0.1:18443/` via a TLS proxy; see [SKILL.md](../SKILL.md) HTTPS cookie proof and [live-github-auth.md](./live-github-auth.md).
- Run `helpers/doctor.sh` first. Refuse a process you did not start. Do not `cleanup.sh` a LIVE-09 wait instance to free a port.
- Never drive Vite `http://127.0.0.1:5173` for API or auth claims.
- Never paste secrets into evidence. Cookie proof records flags only.

## Driving conventions

- Start every recipe from its listed preconditions.
- Click by accessible name. The product UI is Chinese.
- Treat helper invocations as literal.
- Restore fixture data after a mutation. Keep proof artifacts.

## Proof and skip reporting

- Capture the user action and the resulting state.
- UI proof includes a screenshot or the HTML heading plus the matching API body.
- Mutation proof includes a second read.
- Record the feature ID and entry point in `proof.json`.
- Report an unreachable path with the unmet precondition. Do not mark it verified through a different path.

## Feature entry contract

Each feature file starts with an H1 and one paragraph. It then uses exactly these four H2 sections.

1. `Sub-features`
2. `How to get to it (user POV)`
3. `Driving it with Playwright and curl`
4. `Gotchas`

## Features

- [Unconfigured login](./unconfigured-login.md) is the isolated no-App UI. This is the default skill proof.
- [Live GitHub auth](./live-github-auth.md) is B02.6. Launch is local; cookie acceptance is HTTPS-only. Current account-A progress is [CHECKLIST.md](../../../docs/development/2026-09-14-b026-cloud-live-01/CHECKLIST.md).
- [Repository discovery](./repository-discovery.md) is the signed-in workspace list.
- [Projects](./projects.md) is create, list, settings, and original-operation recovery.
- [Model providers](./model-providers.md) is save and close of a provider. No real model HTTP.
