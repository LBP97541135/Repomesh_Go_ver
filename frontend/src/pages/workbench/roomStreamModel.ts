import type { RoomListItemView } from "../../api/contract";
import type { PlanTaskItem } from "../../api/taskTree";
import type { FocusEntry } from "./treeModel";

/** 右侧该看哪条流（spec 2026-09-22 §5）。
 *
 *  - Manager / 规划步骤 / 测试组 / 阶段历史 → Manager 会话（现状）
 *  - **Leader 分组** → 该仓的**团队房**（Leader↔Worker 的任务与结果）
 *  - **任务节点** → 该仓的 **Leader DM 房**（Manager→Leader 的派工与门事件）
 *
 *  房没回读到（清单里没有这个仓 / 该 kind 的房号不存在）一律回落 Manager 会话：
 *  宁可显示一条已知的流，也不摆一间进不去的房（与 api/rooms.ts 里
 *  "只收真有房的条目"同一条口径）。 */
export type StreamTarget =
  | { kind: "manager" }
  | { kind: "room"; roomId: string; roomKind: "team_room" | "leader_dm"; repositoryId: string };

export function streamTargetFor(
  entry: FocusEntry | null,
  rooms: RoomListItemView[] | null,
  tasks: PlanTaskItem[] | null,
): StreamTarget {
  if (entry === null) return { kind: "manager" };

  // task 条目只带 taskId（FocusEntry 不冗余仓库），仓库要从任务列表反查 ——
  // 这是把一条任务落到某个仓上的唯一来源。
  const repositoryId =
    entry.kind === "leader"
      ? entry.repositoryId
      : entry.kind === "task"
        ? ((tasks ?? []).find((t) => t.id === entry.taskId)?.repositoryId ?? "")
        : "";
  if (repositoryId === "") return { kind: "manager" };

  const want: "team_room" | "leader_dm" = entry.kind === "leader" ? "team_room" : "leader_dm";
  const room = (rooms ?? []).find((r) => r.repository_id === repositoryId && r.kind === want);
  if (!room || room.room_id === "") return { kind: "manager" };
  return { kind: "room", roomId: room.room_id, roomKind: want, repositoryId };
}
