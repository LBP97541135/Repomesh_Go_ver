# WSL development migration
User authorized environment setup and migration; Codex desktop Agent/terminal setting changes are reserved for the user. B02 remains paused.

| Phase | Work | Evidence / completion |
| --- | --- | --- |
| A | Inventory source, Git/index, upstream and destination; set Ubuntu default | Baseline JSON and default distro readback |
| B | Install Linux Go, Node, PowerShell, PostgreSQL and bubblewrap | Actual versions, native paths and sandbox probe |
| C | Copy working tree, Git/index and local evidence; retain Windows original | SHA256 manifest, Git state comparison; caches excluded |
| D | Adapt verifier; execute B01 and Linux bundle; Windows to WSL HTTP and Vite updates | Structured verification and package evidence |
| E | Record handoff and exact manual step | README/HANDOFF; desktop switching remains user-owned |

Source owns original files. An isolated worker owns the portable verifier draft; root reviews and integrates it. Target uses its own Linux build outputs, npm dependencies and project marketplace path. Copy excludes platform caches/build outputs, retains historical records and independent upstream. External Windows worktree metadata is archived outside the target repository.

Data shape: each migrated file has relative path, size and SHA256; Git snapshots track HEAD, branch, index hash and porcelain status. Expected target-only configuration changes are recorded separately.

Throughput checkpoint:
- Dominant cost: package downloads and cold builds.
- Parallel work: isolated verifier adaptation while root installs tools and inventories source.
- Ownership: worker edits isolated script; root edits source records and target migration state.
- Completion: filesystem and runtime checks pass; no claim that the reserved desktop UI step was done.

Decisions follow Sequence Work into Verifiable Units and Prove It Works. No commits, pushes or business implementation.

Execution complete: phases A-D verified; phase E recorded. Codex desktop Agent/terminal switching and post-switch model execution remain user-owned. B02 stays paused. See README.md and final-verification.json.
