# GitHub App snapshot tool check

- Scope: fixed GitHub App and installation API reads, plus creation and best-effort revocation of one ephemeral installation token used only for repository enumeration.
- Output review: the success projection contains UTC sample time, API version, stable numeric IDs, bounded login/slug fields, optional public state, repository selection, suspension boolean, permission enums, repository numeric IDs, page counts, and token revoke status. It contains no JWT, token, key/config content, repository name, node ID, raw response, response header, query URL, or exception text.
- Failure review: only fixed local codes, a fixed HTTP-status allowlist, or generic network/HTTP/response codes can be emitted.
- Static validation: `python3 -m py_compile docs/development/2026-09-12-b026-second-account-02/github-app-snapshot.py` passed on 2026-09-12.
- Runtime validation: not run; no GitHub request or token operation occurred while implementing the tool.
