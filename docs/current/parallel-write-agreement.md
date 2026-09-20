# 两个写者共用一条 main 的约定（2026-09-20）

## 背景（为什么需要这份约定）

`LBP97541135/Repomesh_Go_ver` 的 `main` 上**同时有两个写者**：本机的 agent（git author
`吕祎晗`）与另一位协作者/agent（git author `chen-wenhui-kk777`）。push `main` 会**自动
部署到同一台生产服务器**（crazykitties.cn，自托管 runner），部署过程是
「停服 → 迁移 → 安装 → 起服」，中间约 6 秒不可用。

2026-09-20 当天，这种并行已经造成三类事故（都有实证）：

| 事故 | 实证 |
|---|---|
| push 被拒（non-fast-forward） | 多次 `git push` 报 rejected，需要先 rebase |
| **迁移编号撞号 → 部署失败并自动回滚** | `2b592ae 修：迁移版本撞号 —— 我的 0045 顺延为 0046`、`4256d27 修：迁移版本撞号 —— 参与权缓存表顺延为 0047`；06:43 那次部署因两个 0045 直接失败，工作流回滚到上一版 |
| 同文件 rebase 冲突 | `frontend/src/pages/workbench/WorkbenchPage.tsx`（对方的 focusOpen/onViewTrain 与我加的 planState/onInterruptPlan） |
| 互相修对方的 bug | `a3b6fa4 修：补齐「执行中人工打断」漏掉的 props 声明（42acf537 自身 tsc 不干净）` |

## 规矩

1. **push 前必须 rebase**：`git fetch origin && git rebase origin/main`。**绝不 force-push**
   （`main` 是部署源，force-push 会把对方的提交从线上抹掉）。
2. **迁移编号**：
   - 新建迁移前先看 `origin/main` 的**最大编号**，取 +1；
   - **已经落库的迁移文件内容不可再改** —— `repomesh_schema_migrations` 记了 checksum，
     改了内容下一次部署必然失败（`name or checksum differs at version N`）；
   - 撞号时改**自己那份还没落库的**（顺延到下一个空号）。
3. **行尾**：以 HEAD 为准（`git show HEAD:<file>` 数 crlf/lf），提交时用
   `git -c core.autocrlf=false add`；别用 `gofmt -w` 直接写 CRLF 文件（它会把整文件转 LF，
   制造整文件 diff）。
4. **区域划分**（把重叠面压到最小）：本机 agent 主攻 Go 后端/数据/流程与测试；另一位主攻
   前端视觉/导航/交互。必须重叠的文件（如工作台页面）小步提交、频繁 rebase。
5. **不要同时推**：两边同一分钟推会各自触发一次部署，用户会看到两次短暂 502。

## 自检命令

```bash
# 1) 本地迁移编号有没有重复
ls internal/database/migrations | sed -n 's/^\([0-9]\{4\}\)_.*/\1/p' | sort | uniq -d

# 2) origin/main 的最大编号（新建迁移要取 +1）
git ls-tree --name-only origin/main internal/database/migrations/ \
  | sed -n 's#.*/\([0-9]\{4\}\)_.*#\1#p' | sort -n | tail -1

# 3) 服务器已落库的编号（改前先确认它没落库）
ssh -i "$REPOMESH_PEM" root@8.210.190.41 \
  'psql "$REPOMESH_DB_URL" -tAc "select version, name from public.repomesh_schema_migrations order by version desc limit 3"'

# 4) push 前
git fetch origin && git rebase origin/main && git push origin main
```

## 如果只想彻底避免

并行写同一条 main 的成本就是上面这些。更彻底的做法二选一：

- **停一个**（只有一个写者时不存在撞号与冲突）；
- **分工作区/分分支**：另一位换到独立分支或独立 checkout，由一方合并后统一推 `main`。

这两条需要人来定，agent 不能自己停掉另一个写者。