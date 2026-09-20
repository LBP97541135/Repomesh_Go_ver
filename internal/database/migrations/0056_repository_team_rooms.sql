-- 0056: 记住 AgentTeams 团队房。
--
-- 到这一版为止 RepoMesh 把团队和 worker 都建出来了，却把上游回给它的房间号扔掉了
-- （`agent_teams.room_id` 恒空，`repository_teams` 连列都没有），于是「进房间看对话」
-- 这条读面永远没有可进的门。
--
-- 房间号由 AgentTeams 控制器**异步**建立 Matrix 房之后回填到 Team CR 的 status，
-- 所以它不在 `POST /api/v1/teams` 的响应里，只能建完回读 `GET /api/v1/teams/{name}`。

SET LOCAL search_path = public, pg_catalog;

ALTER TABLE public.repository_teams
    ADD COLUMN IF NOT EXISTS team_room_id text,
    ADD COLUMN IF NOT EXISTS leader_dm_room_id text;

COMMENT ON COLUMN public.repository_teams.team_room_id IS
    'AgentTeams Team.Status.TeamRoomID —— 团队房（Matrix room id）。未回读到时保持 NULL。';
COMMENT ON COLUMN public.repository_teams.leader_dm_room_id IS
    'AgentTeams Team.Status.LeaderDMRoomID —— Leader 的 DM 房。未回读到时保持 NULL。';
