# Model providers

Model providers lets a signed-in user save a vendor key into an isolated vault, close an empty slot, and recover the original save. The product does not send a model HTTP request in the adopted B04 scope.

## Sub-features

- `provider-open` opens `/settings/models`.
- `provider-save` submits `保存供应商`. The key field is cleared after send.
- `provider-validation` keeps the user on `/settings/models` for `422 VALIDATION_FAILED`.
- `provider-close` closes an empty slot so a late save cannot create a new key.

## How to get to it (user POV)

- From the workspace, choose `模型设置`.
- Open `/settings/models`.

## Driving it with Playwright and curl

Preconditions:

- Authenticated session and migrated database.
- No operator model API key is required. A fixture value is enough for save-shape proof.
- Do not send a real provider request.
- On the default local HTTP origin the signed-in recipes are `verified-unreachable`. Prerequisite: HTTPS origin in `auth.json` plus `__Host-` session cookie. Source: `internal/web/auth.go`, `internal/web/models.go`.

- **Unconfigured gate.** `helpers/drive-local-gates.sh` opens `/settings/models` on `http://127.0.0.1:<port>`. The page is the auth shell because `/api/session` is 503. `GET /api/model-providers` is 404 `not_implemented`: `registerModels` returns without routes when `Models.Service` is nil (`internal/web/models.go`). Projects still answer 503 `AUTH_NOT_CONFIGURED`. That mismatch is a product gap, not cookie proof.
- **Open.** Muted header `模型设置`. Heading `模型连接`. Button `保存供应商` is visible.
- **Invalid save.** Put `http://gateway.example.invalid/v1` in the **Base URL** field, not in the API key. Stay on `/settings/models`. Alert `这次保存没有受理`. Name and model remain. API key is empty.
- **Valid save.** Use an `https://` fixture URL. `POST /api/model-provider-saves` then the UI opens `/settings/model-saves/{saveId}`. `GET` the same `saveId`. Close with `POST /api/model-provider-saves/{saveId}/close` when testing the empty-slot race.

## Gotchas

- Unconfigured Web does not expose model APIs. Do not treat 404 `not_implemented` as a signed-in save.
- A real vendor key is out of B04 authorization. Do not collect one for this feature.
- Two different S06 names exist. Vault-root S06 is a PostgreSQL case. Adoption text “不采用 S06” is a later repo-permission gate.
- After 30 minutes idle on a configured HTTPS origin, `/settings/models` shows `开始使用 RepoMesh` and `使用 GitHub 登录`. That is not the unconfigured 503 shell. Signed-in save proof needs a session newer than 30 minutes.
