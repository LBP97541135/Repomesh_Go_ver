package humancontrol

import (
	"errors"
	"strings"
	"testing"
)

// validatePolicyDraft 是纯函数：三档的域不变量与四条单条授权规则都在这里判。
// 这些规则同时被前端的三档映射挡着（界面上不该表达出来），所以后端这一层是
// **最后一道**，它漏一条就等于库里会出现一份物化时必被拒的草稿。
func TestValidatePolicyDraft(t *testing.T) {
	grant := PolicyGrant{
		HumanPrincipalID: "acct-1",
		Role:             "project_supervisor",
		CodeAccess:       "read",
		ControlActions:   []string{"approve_checkpoint"},
	}
	repoGrant := PolicyGrant{
		HumanPrincipalID: "acct-1",
		Role:             "repository_supervisor",
		CodeAccess:       "write",
		ControlActions:   []string{"approve_checkpoint"},
		RepositoryID:     ptr("repo_00000000000000000001"),
	}
	allSix := append([]string{}, policyCheckpoints...)

	tests := []struct {
		name       string
		mode       string
		checkpoint []string
		grants     []PolicyGrant
		want       string
	}{
		{name: "auto 零卡点零授权可以", mode: "auto"},
		{name: "auto 带卡点被拒", mode: "auto", checkpoint: []string{"execution"},
			want: "automatic projects cannot require human checkpoints"},
		{name: "supervised 至少要一个卡点", mode: "supervised", grants: []PolicyGrant{grant},
			want: "human-controlled projects require checkpoints"},
		{name: "supervised 至少要一条授权", mode: "supervised", checkpoint: []string{"execution"},
			want: "human-controlled projects require a human grant"},
		{name: "supervised 齐了就行", mode: "supervised", checkpoint: []string{"execution"}, grants: []PolicyGrant{grant}},
		{name: "manual 要全部六个", mode: "manual_controlled", checkpoint: []string{"execution"}, grants: []PolicyGrant{grant},
			want: "manual-controlled projects require every human checkpoint"},
		{name: "manual 六个齐了就行", mode: "manual_controlled", checkpoint: allSix, grants: []PolicyGrant{grant}},
		{name: "未知档位被拒", mode: "sometimes", want: "unknown execution mode"},
		{name: "未知卡点被拒", mode: "supervised", checkpoint: []string{"nope"}, grants: []PolicyGrant{grant},
			want: "unknown checkpoint"},
		{name: "仓库监督人必须给仓库", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "repository_supervisor", CodeAccess: "read", ControlActions: []string{"approve_checkpoint"}}},
			want:   "repository supervisor requires repository scope"},
		{name: "非仓库监督人不许给仓库", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "project_supervisor", CodeAccess: "read", ControlActions: []string{"approve_checkpoint"}, RepositoryID: ptr("repo_00000000000000000001")}},
			want:   "only a repository supervisor may carry a repository scope"},
		{name: "有路径就必须先有仓库", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "project_supervisor", CodeAccess: "read", ControlActions: []string{"approve_checkpoint"}, PathPatterns: []string{"src/**"}}},
			want:   "path patterns require a repository scope"},
		{name: "授权至少要一个动作", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "project_supervisor", CodeAccess: "read"}},
			want:   "human grant requires control actions"},
		{name: "未知动作被拒", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "project_supervisor", CodeAccess: "read", ControlActions: []string{"fly"}}},
			want:   "unknown control action"},
		{name: "未知身份被拒", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "boss", CodeAccess: "read", ControlActions: []string{"approve_checkpoint"}}},
			want:   "unknown human project role"},
		{name: "未知代码权限被拒", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{HumanPrincipalID: "acct-1", Role: "project_supervisor", CodeAccess: "root", ControlActions: []string{"approve_checkpoint"}}},
			want:   "unknown code access level"},
		{name: "授权缺账号被拒", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{{Role: "project_supervisor", CodeAccess: "read", ControlActions: []string{"approve_checkpoint"}}},
			want:   "human grant requires an account"},
		{name: "同人同范围重复被拒", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{grant, grant},
			want:   "duplicate human grant scope"},
		{name: "同人不同仓库范围可以", mode: "supervised", checkpoint: []string{"execution"},
			grants: []PolicyGrant{repoGrant, {
				HumanPrincipalID: "acct-1", Role: "repository_supervisor", CodeAccess: "read",
				ControlActions: []string{"approve_checkpoint"}, RepositoryID: ptr("repo_00000000000000000002"),
			}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePolicyDraft(test.mode, test.checkpoint, test.grants)
			if test.want == "" {
				if err != nil {
					t.Fatalf("应当通过，得到 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当被拒（%s），却通过了", test.want)
			}
			var violation *PolicyViolation
			if !errors.As(err, &violation) {
				t.Fatalf("域不变量必须是 PolicyViolation（→422），得到 %T", err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("文案要能对上界面那一条，得到 %q（期望含 %q）", err.Error(), test.want)
			}
		})
	}
}

// 六个卡点必须与前端 ProjectCheckpoint 逐字一致：两边差一个名字，
// 用户勾上的那一个就会在库里变成「未知卡点」而整份草稿被拒。
func TestPolicyCheckpointsMatchContract(t *testing.T) {
	want := []string{
		"repository_scope", "specification", "execution",
		"validation", "delivery", "exception_escalation",
	}
	if len(policyCheckpoints) != len(want) {
		t.Fatalf("卡点数不符：%d vs %d", len(policyCheckpoints), len(want))
	}
	for index, name := range want {
		if policyCheckpoints[index] != name {
			t.Fatalf("第 %d 个卡点不符：%q vs %q", index, policyCheckpoints[index], name)
		}
	}
}

func ptr[T any](value T) *T { return &value }
