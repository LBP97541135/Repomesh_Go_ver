package skill

import "strings"

// SeedDoc 返回某个种子技能的 SKILL.md 原文（内嵌在二进制里）。
//
// 为什么要有这个出口：规划期的 agent prompt 必须带**真技能文档**，而不是在
// 别处再抄一段"你应该做什么"的话术 —— 抄一份就是第二份真相，技能库改了、
// agent 拿到的还是旧话术（infra 的 Governed Flow 里技能与角色是同一条链上的）。
// 认不出 skillID 时返回空串，调用方据此如实降级（不编一份假的技能文档）。
func SeedDoc(skillID string) string {
	content, err := seedFS.ReadFile("seeddocs/" + skillID + "/SKILL.md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}
