-- M5 dual dispatch introduces the test agent kind alongside the coding CLIs.
ALTER TABLE repomesh_execution.agent_runs DROP CONSTRAINT agent_runs_agent_kind_check;
ALTER TABLE repomesh_execution.agent_runs
    ADD CONSTRAINT agent_runs_agent_kind_check
    CHECK (agent_kind IN ('claude_cli','codex_cli','test_agent'));
