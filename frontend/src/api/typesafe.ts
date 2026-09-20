import { apiRequest } from "./http";

export interface TypeSafeSettingsView {
  projectId: string;
  revision: number;
  enabled: boolean;
  configured: boolean;
  model: string;
  checkedAt: string | null;
  checkStatus: string;
  skillCommit: string;
  skillHash: string;
}
export interface TypeSafeEvaluation {
  id: string;
  projectId: string;
  issueId: string;
  runId: string;
  taskId: string;
  purpose: "test" | "code_review";
  revision: number;
  skillHash: string;
  templateVersion: string;
  input: { claims?: Array<{ id: string; text: string }>; evidence?: string; commit?: string };
  inputHash: string;
  status: "pending" | "completed" | "failed" | "unknown" | "unavailable";
  errorCode: string;
  response?: {
    model: string;
    answers: Record<string, { choice: "supported" | "contradicted" | "insufficient"; confidence: number; probabilities: Record<string, number> }>;
    usage: { input_tokens: number; output_tokens: number };
  };
  latencyMs: number;
  createdAt: string;
}
const path = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/typesafe`;
export const fetchTypeSafeSettings = (projectId: string) => apiRequest<TypeSafeSettingsView>("GET", path(projectId));
export const saveTypeSafeSettings = (projectId: string, input: {
  expectedRevision: number; enabled: boolean; model: string;
  secret: { mode: "keep" | "replace" | "clear"; value?: string };
}) => apiRequest<TypeSafeSettingsView>("POST", path(projectId), input);
export const checkTypeSafeConnection = (projectId: string, expectedRevision: number) =>
  apiRequest<TypeSafeSettingsView>("POST", `${path(projectId)}/check`, { expectedRevision });
export const fetchTypeSafeEvaluations = (projectId: string, issueId: string) =>
  apiRequest<{ items: TypeSafeEvaluation[] }>("GET", `${path(projectId)}/evaluations?issueId=${encodeURIComponent(issueId)}`);

const MESSAGES: Record<string, string> = {
  not_checked: "尚未检查连接", checking: "正在检查连接", ready: "已验证鉴权和推理可用",
  TYPESAFE_UNAVAILABLE: "服务或密钥库暂不可用",
  TYPESAFE_KEY_REQUIRED: "请先保存 API Key",
  TYPESAFE_INVALID_KEY: "API Key 格式不正确",
  TYPESAFE_INVALID_SETTINGS: "配置格式不正确",
  TYPESAFE_KEY_REJECTED: "API Key 未通过 TypeSafe 鉴权",
  TYPESAFE_KEY_UNAVAILABLE: "已保存的密钥不可用",
  TYPESAFE_REVISION_CONFLICT: "配置已变化，请重新读取后再保存",
  TYPESAFE_CHECK_COOLDOWN: "连接检查过于频繁，请一分钟后重试",
  TYPESAFE_OUTCOME_UNKNOWN: "未能确认外部请求结果，未自动重试",
  TYPESAFE_RATE_LIMITED: "TypeSafe 请求限流",
  TYPESAFE_OVERLOADED: "TypeSafe 服务繁忙",
  TYPESAFE_UPSTREAM_FAILED: "TypeSafe 服务返回错误",
  TYPESAFE_INVALID_RESPONSE: "TypeSafe 返回内容未通过校验",
  TYPESAFE_REQUEST_REJECTED: "TypeSafe 拒绝了请求内容",
  TYPESAFE_BROKER_NOT_CONFIGURED: "执行器尚未配置 Jev 调用地址",
  TYPESAFE_NOT_INVOKED: "本次运行未调用 Jev",
  TYPESAFE_HELPER_UNAVAILABLE: "执行器调用工具不可用",
  TYPESAFE_GRANT_REJECTED: "本次运行的调用授权已失效",
  TYPESAFE_RUN_LIMIT: "本次运行已达到调用上限",
};
export function typeSafeMessage(value: unknown): string {
  const raw = value instanceof Error ? value.message : String(value);
  const code = raw.match(/TYPESAFE_[A-Z_]+/)?.[0] ?? raw;
  return MESSAGES[code] ?? (code.startsWith("TYPESAFE_") ? "Jev 调用未完成" : "操作失败，请重新读取后重试");
}
