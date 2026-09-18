# Linux 接手与 B02 开工核对

## 工作步骤

- [x] Read the Principles section of the poteto-mode skill.
- [x] Phase A: Frame
- [x] Phase B: Design the workflow
- [x] Phase C: Run the loop
- [x] 核实 Linux、工程路径、桌面 SSH 项目和工具位置，保存已有修改清单。
- [x] 完整阅读用户指定的五组入口，核对 B02 的采用来源和跨批依赖。
- [x] 更新当前交接，给出 B02 最小实施范围及前置清单。
- [ ] 按明确采用范围实施 B02，运行相关检查及真实登录验收。BLOCKED，候选待确认采用；真实 App 未配置另阻止集成验收。
- [x] 由非主要实施者复核交接、证据与下一步范围。
- [x] Phase D: Keep the audit trail
- [x] Phase E: Verify and hand back

## 本轮通过条件

接手核对通过要求实际工具运行于指定 Linux 目录，桌面项目指向同一 SSH 主机与目录，原有文件修改可核对，并且交接准确区分已验证的环境和未实现的认证。

B02 通过条件沿施工表。先确定认证候选的采用范围，即可实施和验证本地单元。真实登录及页面集成验收另需 GitHub App 和 HTTPS 回调。环境核对通过不代表 B02 通过。

## 吞吐检查

- Blocking first steps. 认证候选的采用确认是代码开工前置。用户已确认 GitHub App 尚未配置，这属于真实集成验收前置，采用后本地实现与验证可以先行。
- Independent workstreams. 主代理读取采用文档、维护当前交接和本轮证据；只读代理独立核对代码接点、跨批依赖和配置存在性；最后由另一模型独立复核实际文件。
- Shared mutable state. 主代理是本轮唯一文件编辑者。保留已有修改，不整体暂存、提交、推送，不更新上游源码。
- Smallest safe decomposition. 本轮先交付可验证的环境接手和 B02 前置记录。B02 的秘密基础与发现续扫必须纳入本批范围；模型配置、预算政策和运行执行保留各自批次。

## 验证安排

本轮若只改文档，核对本地链接、命令与源码事实、已有修改保留情况，并独立复核。沿用已保存的 B00/B01 证据，不重复运行数据库或历史实验。B02 采用范围确认后，按子单元实施，每单元自测并独立验证后推进；真实配置齐备后补齐 OAuth 与页面验收。
