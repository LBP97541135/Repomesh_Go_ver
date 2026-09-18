# 开发准备前的文档归档

日期：2026-09-12。用户要求归档无用文档并整理开发前阅读与行动。本次从活跃目录移走 **19 份历史文档和 4 份被替代 HTML**；另保留 6 份精简入口的整理前快照。当前阅读从[开发指南](../../plan/DEVELOPMENT-START.md)、[当前交接](../../current/HANDOFF.md)和[现行索引](../../current/README.md)进入。

归档不删除采用原话或旧证据，不改变任何方案的采用状态。全部 ADR、现行唯一契约、未采用关键方案、暂缓 Skill 专题、工程验收和上游证据索引均保留。`current` 从 66 份 Markdown 减至 48 份（移走 19 份、新增 1 份开发指南）；原型目录保留 11 份原稿及生成入口。

根 `.gitignore` 已放行本目录和归档总导航，使移走的文档能够随后续提交保留；其他历史目录仍按原规则忽略。本次没有暂存或提交文件。

## 原始字节与导航副本

[originals.zip](originals.zip)保存本次涉及导航迁移和入口重写前的 **66 份文件原始字节**，包括搬走文件、重写入口及被重定位链接的历史通信／报告。下方归档 Markdown 只做相对链接重定位；旧模型弹窗只修正搬迁后失效的返回入口，原始字节仍在 ZIP 内。正文中的原角色、阶段、代码片段、检查数字及采用原话保留历史含义。

[manifest.json](manifest.json)记录原路径、归档路径、迁移前 SHA-256、归档副本 SHA-256，以及导航迁移当时的记录。当前入口之后已精简，manifest 的导航 after_sha256 表示迁移步骤完成时的字节，不声称是此后重写入口的最终哈希。历史审查 JSON 中的路径、哈希和检查结果没有改写；通过本清单定位已归档来源。

## 移出活跃目录的文件

| 原路径 | 归档副本 | 原因 |
| --- | --- | --- |
| `docs/current/design-communication.md` | [design-communication.md](docs/current/design-communication.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/design-communication-page-log.md` | [design-communication-page-log.md](docs/current/design-communication-page-log.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/design-communication-backend-log.md` | [design-communication-backend-log.md](docs/current/design-communication-backend-log.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/design-communication-page-2026-09-10.md` | [design-communication-page-2026-09-10.md](docs/current/design-communication-page-2026-09-10.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/design-communication-backend-2026-09-10.md` | [design-communication-backend-2026-09-10.md](docs/current/design-communication-backend-2026-09-10.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/design-communication-page-2026-09-11.md` | [design-communication-page-2026-09-11.md](docs/current/design-communication-page-2026-09-11.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/design-communication-backend-2026-09-11.md` | [design-communication-backend-2026-09-11.md](docs/current/design-communication-backend-2026-09-11.md) | 旧任务通信、角色与轮次；保留采用原话供溯源。 |
| `docs/current/NEXT-SESSION-PROMPT.md` | [NEXT-SESSION-PROMPT.md](docs/current/NEXT-SESSION-PROMPT.md) | 旧会话接手提示；开发阅读已由新指南承接。 |
| `docs/current/NEXT-BACKEND-SESSION-PROMPT.md` | [NEXT-BACKEND-SESSION-PROMPT.md](docs/current/NEXT-BACKEND-SESSION-PROMPT.md) | 旧会话接手提示；开发阅读已由新指南承接。 |
| `docs/current/page-interface-prototype.md` | [page-interface-prototype.md](docs/current/page-interface-prototype.md) | A／B／C 旧布局已被否定。 |
| `docs/current/draft-issue-and-room-entry-design.md` | [draft-issue-and-room-entry-design.md](docs/current/draft-issue-and-room-entry-design.md) | 旧草稿绑定已被会话／Issue 分离替代。 |
| `docs/current/project-first-entry-design.md` | [project-first-entry-design.md](docs/current/project-first-entry-design.md) | 旧入口过程由当前项目配置及会话专题承接。 |
| `docs/current/changeset-design-discussion.md` | [changeset-design-discussion.md](docs/current/changeset-design-discussion.md) | 仅为跳转，现行 ChangeSet 规则与结构仍保留。 |
| `docs/current/document-review-2026-09-09.md` | [document-review-2026-09-09.md](docs/current/document-review-2026-09-09.md) | 09-09 历史审查记录，非当前实现规范。 |
| `docs/current/scaffold-source-inventory.md` | [scaffold-source-inventory.md](docs/current/scaffold-source-inventory.md) | 骨架阶段的阅读／审查记录；工程验收与上游证据索引仍在 current。 |
| `docs/current/scaffold-adr-review.md` | [scaffold-adr-review.md](docs/current/scaffold-adr-review.md) | 骨架阶段的阅读／审查记录；工程验收与上游证据索引仍在 current。 |
| `docs/current/scaffold-page-backend-review.md` | [scaffold-page-backend-review.md](docs/current/scaffold-page-backend-review.md) | 骨架阶段的阅读／审查记录；工程验收与上游证据索引仍在 current。 |
| `docs/current/HANDOFF-BACKEND-DESIGN-2026-09-11.md` | [HANDOFF-BACKEND-DESIGN-2026-09-11.md](docs/current/HANDOFF-BACKEND-DESIGN-2026-09-11.md) | 旧累计交接；当前角色交接保留 B01—B08 去向。 |
| `docs/current/first-development-todo.md` | [first-development-todo.md](docs/current/first-development-todo.md) | 原范围确认与旧阶段进度；开发指南承接当前顺序，原采用依据仍可查。 |
| `docs/prototypes/repomesh-conversation-navigation-prototype.html` | [repomesh-conversation-navigation-prototype.html](docs/prototypes/repomesh-conversation-navigation-prototype.html) | 固定导航 r1 已被悬浮入口 r2 替代。 |
| `docs/prototypes/repomesh-model-settings-prototype.html` | [repomesh-model-settings-prototype.html](docs/prototypes/repomesh-model-settings-prototype.html) | 模型连接弹窗 r2 已被供应商分栏 r3 替代。 |
| `docs/prototypes/repomesh-project-first.html` | [repomesh-project-first.html](docs/prototypes/repomesh-project-first.html) | 会话／Issue 分离前的历史页面。 |
| `docs/prototypes/repomesh-rooms-prototype.html` | [repomesh-rooms-prototype.html](docs/prototypes/repomesh-rooms-prototype.html) | 旧草稿与房间绑定的历史页面。 |

## 精简前的入口快照

| 当前路径 | 整理前全文 |
| --- | --- |
| `docs/current/HANDOFF.md` | [快照](docs/current/HANDOFF.md) |
| `docs/current/README.md` | [快照](docs/current/README.md) |
| `docs/current/HANDOFF-PAGE-API-DESIGN.md` | [快照](docs/current/HANDOFF-PAGE-API-DESIGN.md) |
| `docs/current/HANDOFF-BACKEND-DESIGN.md` | [快照](docs/current/HANDOFF-BACKEND-DESIGN.md) |
| `docs/prototypes/README.md` | [快照](docs/prototypes/README.md) |
| `docs/README.md` | [快照](docs/README.md) |

## 本次检查

实际核对 2,460 个本地链接，未引入新断链；更早归档中原有 70 次缺失引用保留并单列。29 项归档／快照哈希与 66 份原始字节校验通过，27 个脚本单元和 30 段现行 JSON 示例可解析。五个嵌入家族与生成结果一致，15 个原稿入口均可定位，当前 11 份原 HTML 字节未改。

非主要整理者独立复核了开发指南、现行索引、三份交接和本说明：F01—F15／B01—B08 去向保留，关键候选和暂缓范围未改成已采用，真实 Issue 的配置关联未被推迟到运行接入之后。审查中的供应商费用摘要已改为“不自动重发可能收费请求”，不承诺外部计费恰好一次。

[归档验证](verification.json)记录原始包／副本哈希、移动与保留范围、本地链接及生成结果检查。[检查脚本](verify_archive.py)可重跑，不覆盖此前审查证据。仅验证文档、导航与生成结果，没有执行产品构建、数据库、真实账号／模型或上游实验。

新生成的串联预览仍保留五个家族和全部十五份来源的查看入口，其中四条明确标为历史归档。当前十一份原 HTML 未修改；归档模型旧稿的返回路径已改指当前入口。
