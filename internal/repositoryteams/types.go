// Package repositoryteams defines browser-safe repository Team projections.
package repositoryteams

import "strconv"

type Snapshot struct {
	RepositoryID   string   `json:"repository_id"`
	RepositoryName string   `json:"repository_name"`
	RosterRevision int64    `json:"roster_revision"`
	RuntimeStatus  string   `json:"runtime_status"`
	Leader         Member   `json:"leader"`
	Workers        []Member `json:"workers"`
	// AgentTeams 团队房与 Leader DM 房。空串 = 还没回读到（房间由控制器异步建），
	// 不拿仓库名或团队名拼一个假的出来。
	TeamRoomID     string `json:"team_room_id"`
	LeaderDMRoomID string `json:"leader_dm_room_id"`
	// RuntimeNote 说明**这次运行阶段观察的完整性**（空串 = 全部读到了）。
	//
	// 2026-09-21：运行阶段是**观察**，编制是**持久化事实**。观察失败不该销毁事实，
	// 但也不能假装都读到了 —— 所以这里如实写明有几个成员没读到，让界面能说清。
	RuntimeNote string `json:"runtime_note,omitempty"`
}

type Member struct {
	ID              string `json:"id"`
	ResourceName    string `json:"resource_name"`
	DisplayLabel    string `json:"display_label"`
	RuntimePhase    string `json:"runtime_phase"`
	ActiveTaskCount int    `json:"active_task_count"`
}

type ChangeCommand struct {
	WorkerCount    int   `json:"worker_count"`
	RosterRevision int64 `json:"roster_revision"`
}

// BusyWorkersError exposes only stable browser display labels for Workers
// selected by a rejected scale-down operation.
type BusyWorkersError struct {
	Labels []string
}

func (e *BusyWorkersError) Error() string {
	return ErrBusyWorkers.Error()
}

func (e *BusyWorkersError) Unwrap() error {
	return ErrBusyWorkers
}

func WorkerLabel(index int) string {
	if index <= 0 {
		return ""
	}
	return "W" + strconv.Itoa(index)
}
