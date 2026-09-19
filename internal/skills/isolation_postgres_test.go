package skill

import (
	"context"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 技能库按空间隔离（迁移 0039）。
//
// 修复前：public.skills 是**全局一份**（UNIQUE(name)），RegisterSkill 还是
// ON CONFLICT (name) DO UPDATE —— 公有部署（一账号一空间）下，任何登录账号
// 都能看到别人的技能，还能按名字把别人的技能覆盖掉。
//
// 现在：organization_id 为 NULL = 全局种子技能（所有人可见）；非空 = 空间私有。
func TestPostgresSkillsAreSpaceScoped(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		orgA = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa"
		orgB = "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb"
	)
	store := &Store{Pool: pool}

	// 两个空间各登记一把**同名**技能：必须各自独立，不许互相覆盖。
	skillA, err := store.RegisterSkill(ctx, orgA, "满减校验", "促销场景", "worker", "账号A")
	if err != nil {
		t.Fatalf("RegisterSkill(A): %v", err)
	}
	skillB, err := store.RegisterSkill(ctx, orgB, "满减校验", "促销场景", "worker", "账号B")
	if err != nil {
		t.Fatalf("RegisterSkill(B): %v", err)
	}
	if skillA.ID == skillB.ID {
		t.Fatal("两个空间的同名技能必须是两条记录（此前会被 ON CONFLICT(name) 覆盖）")
	}

	// 按名字解析：各自解析到自己的那把。
	resolvedA, err := store.GetSkillByName(ctx, orgA, "满减校验")
	if err != nil || resolvedA.ID != skillA.ID {
		t.Fatalf("A 应解析到自己的技能，得到 %v (err=%v)", resolvedA, err)
	}
	resolvedB, err := store.GetSkillByName(ctx, orgB, "满减校验")
	if err != nil || resolvedB.ID != skillB.ID {
		t.Fatalf("B 应解析到自己的技能，得到 %v (err=%v)", resolvedB, err)
	}

	// 全局种子技能：三个视角都能看到。
	if _, err := store.RegisterSkill(ctx, "", "全局种子技能", "通用", "leader", "system-seed"); err != nil {
		t.Fatalf("RegisterSkill(全局): %v", err)
	}
	for name, org := range map[string]string{"A": orgA, "B": orgB, "无空间": ""} {
		list, err := store.ListSkills(ctx, org)
		if err != nil {
			t.Fatalf("ListSkills(%s): %v", name, err)
		}
		if !hasSkill(list, "全局种子技能") {
			t.Fatalf("%s 应看到全局种子技能，得到 %d 条", name, len(list))
		}
		if org == "" && hasSkill(list, "满减校验") {
			t.Fatalf("没有空间的账号不该看到任何空间私有技能")
		}
	}

	// A 的列表里有自己那把，且**只有**自己那把同名技能（不能混进 B 的）。
	listA, err := store.ListSkills(ctx, orgA)
	if err != nil {
		t.Fatalf("ListSkills(A): %v", err)
	}
	matched := 0
	for _, sk := range listA {
		if sk.Name == "满减校验" {
			matched++
			if sk.ID != skillA.ID {
				t.Fatalf("A 的列表混进了别人的同名技能：%s", sk.ID)
			}
		}
	}
	if matched != 1 {
		t.Fatalf("A 应恰好看到一把「满减校验」，得到 %d 把", matched)
	}
}

func hasSkill(list []Skill, name string) bool {
	for _, sk := range list {
		if sk.Name == name {
			return true
		}
	}
	return false
}
