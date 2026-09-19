package main

import (
	"context"

	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/tasks"
)

// replanAdapter 把发现链的重排请求接到任务轴的落库入口。
//
// 两个域各持自己的类型（discovery 不 import tasks），跨域只走这层接口：
// 发现链的 ReplanRequest（仓库名 + 任务全集 + 仓库级依赖）在这里折成
// tasks.ReplanCommand，落库交给 tasks.PostgresStore.Replan —— 全量快照替换、
// 任务轴迁移与双轴挂钩（决策链同事务）都在那边，这里不重复实现。
type replanAdapter struct {
	store *tasks.PostgresStore
}

func (a replanAdapter) ApplyReplan(ctx context.Context, req discovery.ReplanRequest) (discovery.ReplanResult, error) {
	snapshots := make([]tasks.TaskSnapshot, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		snapshots = append(snapshots, tasks.TaskSnapshot{
			TaskUID:      task.TaskUID,
			RepositoryID: task.Repository,
			Title:        task.Title,
			Instruction:  task.Instruction,
			Acceptance:   task.Acceptance,
		})
	}
	revision, err := a.store.Replan(ctx, tasks.ReplanCommand{
		PlanID:         req.PlanID,
		Actor:          req.Actor,
		Reason:         req.Reason,
		UpstreamRef:    req.UpstreamRef,
		IdempotencyKey: req.IdempotencyKey,
		Repositories:   req.Repositories,
		Tasks:          snapshots,
		DAG:            req.DAG,
	})
	if err != nil {
		return discovery.ReplanResult{}, err
	}
	return discovery.ReplanResult{
		ResultVersion:   revision.ResultVersion,
		CreatedTasks:    revision.CreatedTasks,
		SupersededTasks: revision.SupersededTasks,
	}, nil
}
