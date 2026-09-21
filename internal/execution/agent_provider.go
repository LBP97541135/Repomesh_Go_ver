package execution

import (
	"os"
	"strings"
)

// agentProviderFlags 把 agent 命令行里写死的 minimax 提供方换成配置的提供方。
//
// 2026-09-22 线上故障：所有 agent run 以 exit=1 结束，stderr 是
// "exceeded retry limit, last status: 429 Too Many Requests" —— 提供方在限流，
// codex 重试 5 次后放弃。而 -c model_provider=minimax 在 coordinator 的 4 处
// 写死（ledger.go x2、planning.go、integration.go），换提供方只能改代码。
//
// **为什么挂在这里**：那 4 处各自 fmt.Sprintf 拼命令行，参数个数与顺序各不相同；
// 逐一手改容易漏。而 ClaimAgentLaunch 是**唯一**把 command 从库里取出来交给
// 执行器的地方 —— 在这里改写，一处覆盖全部 4 个写入点，且不碰那些 Sprintf。
//
// 配置（不配则行为一字不变，仍是 minimax）：
//
//	REPOMESH_AGENT_PROVIDER           提供方名
//	REPOMESH_AGENT_PROVIDER_BASE_URL  该提供方的 base_url
//	REPOMESH_AGENT_PROVIDER_ENV_KEY   取 key 的环境变量名（默认 REPOMESH_AGENT_PROVIDER_KEY）
func agentProviderFlags(command string) string {
	const hardcoded = "-c model_provider=minimax"
	if !strings.Contains(command, hardcoded) {
		return command
	}
	provider := strings.TrimSpace(os.Getenv("REPOMESH_AGENT_PROVIDER"))
	if provider == "" || provider == "minimax" {
		return command
	}
	// codex 只认它自己配置里的提供方定义：光给名字不够，必须把 base_url 与
	// env_key 一并注入，否则它会去找一个不存在的内置提供方。
	flags := "-c model_provider=" + provider
	if base := strings.TrimSpace(os.Getenv("REPOMESH_AGENT_PROVIDER_BASE_URL")); base != "" {
		flags += " -c model_providers." + provider + ".base_url=" + base
	}
	envKey := strings.TrimSpace(os.Getenv("REPOMESH_AGENT_PROVIDER_ENV_KEY"))
	if envKey == "" {
		envKey = "REPOMESH_AGENT_PROVIDER_KEY"
	}
	flags += " -c model_providers." + provider + ".env_key=" + envKey
	return strings.Replace(command, hardcoded, flags, 1)
}
