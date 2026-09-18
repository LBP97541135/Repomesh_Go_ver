# 独立上游源码

`AgentTeams/` 是按用户要求重新克隆的官方 AgentTeams 仓库，保留自己的 `.git`、完整克隆历史和干净工作树。它位于 RepoMesh 目录内，供后续源码阅读和受控适配使用，尚未连接 RepoMesh 运行入口。

```text
third_party/
├── README.md
├── go.mod                    # 隔离上游源码，不是产品模块
├── agentteams-source.json    # 来源、核对时点和完整 commit
└── AgentTeams/               # 独立 Git clone，父仓库忽略
```

2026-09-10 本次 GitHub 查询的默认分支为 `main`，提交为 `517caff9280242a00a4d4c06365352b9e41659c6`。新克隆 HEAD 与 GitHub 查询一致，工作树干净，`git fsck --full` 通过。这是本次查询时的最新 main 源码，不代表最新正式 release，也不构成适配或业务验收。

RepoMesh 的根 `.gitignore` 排除整个 `third_party/AgentTeams/`，防止嵌套仓库被误加为 gitlink 或将整套上游文件提交进产品仓库。这里没有设置 Git submodule；因此克隆 RepoMesh 本身不会自动带回上游文件，必须另行克隆。来源和确定提交由[来源记录](agentteams-source.json)保存。

在没有此目录的新工作副本中，从 RepoMesh 根目录重建当前记录的源码：

```powershell
git clone --branch main https://github.com/agentscope-ai/AgentTeams.git third_party/AgentTeams
git -C third_party/AgentTeams checkout --detach 517caff9280242a00a4d4c06365352b9e41659c6
```

当前本机 clone 保持在 `main`；上面的 detached checkout 仅用于复现记录中的确定版本。日后更新前应检查上游工作树和本地提交，核对新的远端提交并同步来源记录，不在构建脚本中自动追逐远端分支。正式补丁、镜像及部署方案仍待后续接入阶段确定。

`third_party/go.mod` 使根目录的 `go build/test/vet ./...` 不遍历上游源码。上游按自己的构建上下文维护，不属于 RepoMesh 的三个产品二进制，也没有加入本轮发布脚本或安装运行依赖。不要在此边界 module 运行 tidy 来聚合上游依赖。

旧独立目录 `D:\Project4work\AgentTeams` 包含未提交笔记、报告、原型和素材。删除前对全部 1,319 个文件（含 `.git`、未跟踪及忽略文件）生成 ZIP，逐文件 SHA-256 核对归档内容，并在移除前再次确认源文件未改变。备份为：

```text
D:\Project4work\_repo_backups\AgentTeams-20260910T050623Z.zip
D:\Project4work\_repo_backups\AgentTeams-20260910T050623Z.manifest.json
```

永久递归删除命令曾被自动审批以 `blocked by policy` 拒绝，随后使用可恢复的 Windows 回收站移除旧目录，操作成功，原路径已不存在。备份保持独立，没有把旧本地修改混入新克隆。

两轮 `validation/agentteams-*/upstream/` 及其报告、evidence、scripts、运行目录均保留，不移动、不更新、不重跑。新源码碰巧与第二轮锁定 SHA 相同，也不能据此宣称已重复通过实验。架构、Graph 进程内边界、Skill 暂缓和 Python 分析方向保持原决定。
