# Account B fixture resume helper check

- Scope: resume exactly one already-filled account B `/new` page. The generated name remains in memory and must match the fixed random-name shape.
- Pre-submit guards: exact B meta login and owner text, Private visibility, empty Description, fixed README/gitignore/license defaults, no visible invalid fields or known error semantics, visible name-available semantic, and one enabled exact Create button.
- Postconditions: exact in-memory owner/name path, account B meta login, positive repository ID, a fixed private indicator, then account A meta login plus HTTP 404 and a fixed not-found variant.
- Evidence and cleanup: the adjacent evidence is created exclusively as 0600 and fsynced before cleanup. Only other account B `/new` pages whose actual fixed name textbox remains empty and whose exact Create button is visible are then closed; the created B page and A 404 page remain open.
- Output: evidence and stdout contain no repository name, URL, form value, DOM, cookie, query, fragment, or raw error.
- Static validation: `node --check docs/development/2026-09-12-b026-second-account-02/resume-b-fixture.mjs` passed on 2026-09-12.
- Runtime validation: not run; no CDP connection, click, submit, or page close occurred during implementation.
