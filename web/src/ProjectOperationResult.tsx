import { useEffect, useState } from "react";
import type { ApiError } from "./api";
import type { AuthenticatedPageProps } from "./projectPages";
import { projectErrorMessage, readProject } from "./projectApi";
import type { ProjectView } from "./projectApi";
import { clearOperationInputs, operationStorageKey } from "./projectRecovery";
import type { CommittedOperation } from "./projectRecovery";
import { Observation } from "./shared";

export type OperationResultProps = AuthenticatedPageProps & { result: CommittedOperation };

export function ProjectOperationResult({ result, session, auth, navigate }: OperationResultProps) {
  const receipt = result.receipt;
  const [current, setCurrent] = useState<{ kind: "loading" } | { kind: "ready"; value: ProjectView } | { kind: "error"; error: ApiError } | { kind: "inaccessible" }>({ kind: "loading" });

  useEffect(() => {
    try { sessionStorage.removeItem(operationStorageKey(result.identity)); } catch { /* Browser storage may be unavailable. */ }
    const expected = auth.currentGeneration();
    const controller = new AbortController();
    void readProject({ projectId: receipt.projectId, signal: controller.signal }).then((response) => {
      if (controller.signal.aborted || !auth.isCurrent(expected)) return;
      if (response.kind === "ok") setCurrent({ kind: "ready", value: response.value });
      else if (response.status === 401) {
        auth.unauthorized(expected);
        setCurrent({ kind: "inaccessible" });
      } else if (response.code === "RESOURCE_NOT_FOUND") {
        clearOperationInputs({ actor: session.user.id, projectId: receipt.projectId });
        setCurrent({ kind: "inaccessible" });
      } else setCurrent({ kind: "error", error: response });
    });
    return () => controller.abort();
  }, [auth, receipt.projectId, result.identity, session.user.id]);

  const time = result.kind === "project_create" ? result.receipt.createdAt : result.receipt.updatedAt;
  return <section className="receipt panel" aria-labelledby="receipt-title">
    <p className="eyebrow">原操作回执</p><h1 id="receipt-title">本次已保存</h1>
    <dl><div><dt>操作</dt><dd>{result.kind === "project_create" ? "创建项目" : "更新项目"}</dd></div><div><dt>本次项目修订</dt><dd><code>{receipt.projectRevision}</code></dd></div><div><dt>提交时间</dt><dd><Observation at={time} /></dd></div></dl>
    {current.kind === "loading" && <p role="status">正在另行读取当前项目…</p>}
    {current.kind === "ready" && <div className="current-result"><p>{current.value.projectRevision === receipt.projectRevision ? "当前项目仍是本次保存的修订。" : "记录后来有更新。本次回执保持原修订，下面入口会读取当前项目。"}</p><button className="primary compact" onClick={() => navigate(`/projects/${encodeURIComponent(receipt.projectId)}`)}>查看当前项目</button></div>}
    {current.kind === "error" && <div><p className="notice" role="alert">{projectErrorMessage(current.error)}</p><button onClick={() => navigate(`/projects/${encodeURIComponent(receipt.projectId)}`)}>稍后查看当前项目</button></div>}
    {current.kind === "inaccessible" && <p className="notice">当前无法访问项目。原回执不授予项目读取权限。</p>}
  </section>;
}
