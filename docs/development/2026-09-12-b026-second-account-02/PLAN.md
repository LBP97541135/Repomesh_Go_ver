# 第二账号验收 02 执行编排

当前 READY_TO_RUN；规划由 GPT-6 Astra 完成，不执行外部操作。GATE 已有独立复核 PASS，其余场景 NOT_RUN。第一步之前重新只读核对 r3 实际服务版本和固定 HTTPS origin，不能按历史 PID 假定；不重新启动自然刷新 observer 或 keeper，不修改旧结果。

## 白名单与会话安排

A 为 catbobyman，B 为 bohanxu111。origin 为 https://repomesh.bohanxu.me:8443，App 为 repomesh-bohan-b026；A installation 161172403，A-owned 样本稳定 ID 1367444901。B-only ID 和 B installation ID 必须实际创建后填写，不预设。仓库显示名称只在受限浏览器现场使用，证据只保存名称是否出现的布尔判断，不能保存名称、搜索文本或含名称的路径。

每条事件只允许：UTC时间、步骤名、账号角色、actor ID、仓库/安装/attempt稳定ID、连接epoch/revision、session generation及有效标志、HTTP状态、业务错误码、用户参与/App能力状态与观察时间、coverage、页号/结果数/是否有下一页、ID存在或名称泄露布尔值、配置权限枚举、用户完成标志。分页令牌仅在内存中用于下一请求，证据不保存原值。OAuth返回仅存固定路径和状态，不存查询参数；不抓取原始网络记录或未裁剪截图。禁止持久化 Cookie、code、state、csrfToken、authorizationUrl、token、秘密、PEM、数据库连接及私仓名。

B 使用独立浏览器上下文 B，A 使用独立上下文 A；不复制会话材料。B 初次登录获得的真实 RepoMesh 会话保持至 LIVE-08 完成，期间不注销、不重新登录或重连 B。账号持有人可在 A 上下文的 GitHub 选择器中选择 B，RepoMesh A 的会话必须仍为原 A；不能在 A 上下文先执行 RepoMesh B 登录。操作只处理此次自有上下文，不清理既有 profile。

## 串行执行及每步证据

| 步骤 | 实际动作 | 动作前白名单 | 动作后白名单及判据 |
| --- | --- | --- | --- |
| 0 | 只读接管 GATE 与 r3，建立恢复待办 | preflight核心哈希、独立复核PASS、旧session401 | 实际版本、origin、开始时间；不把新登录写入旧刷新证据 |
| 1 | A 打开专用 App 设置，进入 Advanced 将 private 改 public，持有人确认 | App private；A安装161172403为selected，仅1367444901；metadata read/contents write/pull requests write | public及完成时间；原A范围未扩大。前态不符则先记录差异并暂停样本操作 |
| 2 | B 打开 https://github.com/new 建立专用私仓；打开 https://github.com/apps/repomesh-bohan-b026/installations/new，仅选新仓安装 | 当前GitHub角色B、创建前无B样本 | 实际B-only稳定ID、private、A不是协作者、B安装ID、selected且仅该ID；由A只读检查不可访问的布尔结论，不保存URL中的名称 |
| 3 | A 在1367444901实际仓库的 Settings/Collaborators 发邀请，B在实际邀请页接受 | B无协作者权限、A仓稳定ID | 邀请发出和接受的各自UTC时间、B身份、接受完成；邀请URL只在浏览器中使用 |
| 4 | B 在上下文B打开RepoMesh /login，亲自登录并授权；发起新发现 | GitHub角色B、邀请已接受、B安装仅选B-only | B actor/generation/有效会话、B-only稳定ID；A-owned用户参与allowed和App能力allowed及各自observedAt；保留B会话 |
| 5 | A 在上下文A建立新的RepoMesh有效会话，查询初始完整仓库分页 | 确认上下文A独立、GitHub角色A | A actor、epoch、revision、generation、有效session；页数、稳定ID集合及B-only缺席/名称未出现 |
| 6 | 从A工作区发起重新连接GitHub，持有人在GitHub选择器选B并完成返回 | 紧邻操作的A actor/epoch/revision/generation有效快照 | attempt终态rejected、ACCOUNT_MISMATCH；A actor/epoch/revision/generation不变且原session有效；结果页B身份/数据未出现 |
| 7 | A重新查询完整分页直到无下一页 | LIVE-05返回后原A有效会话 | 每页状态/页号/条数/next存在标志；去重ID集合不含B-only，页面与响应名称泄露均false；不得用搜索空结果代替完整分页 |
| 8 | 返回原上下文B，同一会话再次真实查询A-owned得到allowed | B actor/generation与步骤4相同、无新增B登录或重连 | 保存本次用户参与allowed的API observedAt为allowedObservedAt，及请求/响应时间、App allowed；它是撤权计时基线 |
| 9 | A移除B对1367444901的协作者权限 | B本次allowed证据已落盘 | 保存GitHub已完成移除的removedAt、仓库ID、collaboratorPresent=false；B会话不动 |
| 10 | 等待至距allowedObservedAt严格超过60秒，原B会话发起新发现并取完相关分页 | B actor/generation仍同一；没有时钟/缓存期限修改 | recheckedAt与actualElapsedSeconds=(recheckedAt-allowedObservedAt)；必须>60且重查晚于removedAt；HTTP成功结果不披露旧名称，保存ID是否缺席及coverage |
| 11 | 原B会话再次真实重查/分页，确认旧名不重新出现 | 第一次撤权查询成功、原B身份有效 | 第二次查询时间、session连续性、名称泄露false；不能将503/unknown当作不披露通过 |
| 12 | 无论此前成功、失败或阻塞都进入下面恢复 | 逐项已变更和未完成列表 | 每项真实最终状态、检查时间和证据；恢复失败不能标PASS |

如步骤4或8得不到真实双allowed，记录实际BLOCKED/FAIL，不以B-only或App denied代替A-owned撤权前态。任何成功错误重连、A身份/连接被替换、A会话失效或跨账号披露记FAIL并立即转恢复。若B会话在撤权前失效，本轮同会话场景未满足，保留结果并转恢复；不得悄悄重新登录继续计时。暂时503保存实际状态，正常重读成功结果后再评定。需要额外审批由持有人完成，不绕过。

## 必做恢复

成功、失败、中断、审批阻塞都执行；维护每项NOT_RUN/完成/失败及时间，不因为后续步骤未开始而跳过已发生变更的恢复。外部交互不可用时保留RESTORE BLOCKED和待办，不声称已恢复。

1. A恢复App为private并核实，记录可见性前后状态及时间。
2. B在 https://github.com/settings/installations 打开本次实际安装并亲自卸载；确认该installation已不存在，不能删除A的161172403。
3. A再次确认B已不是1367444901协作者；若未移除或尚有待接受邀请，移除/撤销该临时权限或邀请并核实。
4. B在 https://github.com/settings/apps/authorizations 检查专用App用户授权，存在则由持有人撤销，不存在则记录实查缺席。原计划要求的授权清理不是可跳过项。
5. A在 https://github.com/settings/installations/161172403 实际复核selected范围仅1367444901，metadata read、contents write、pull requests write；不重建安装或扩大范围。若需权限批准由持有人完成后重查。
6. 正常注销本轮仍有效的RepoMesh A/B会话，记录注销HTTP状态及session401。B-only私仓是否保留由持有人决定，不自行删除；无论保留与否不得继续安装专用App或给A访问。
7. 未执行主要操作的模型独立复核GATE、LIVE-05、LIVE-08、时间差、完整分页、会话连续性、裁剪与全部恢复。再合并检查LIVE-01至LIVE-10所有必需范围；无开放P0/P1/P2且完整通过才评定B02.6和B02 VERIFIED。后续A登录属于新工作会话，不能反写刷新证据。

第一准确入口为 https://github.com/settings/apps/repomesh-bohan-b026 ，来自已存配置记录；具体私仓与邀请入口须由运行时受限浏览器定位，不能猜名称写入文档。持有人点击建议：“请用账号A打开这个App的Advanced设置，将可见性改为Public并完成确认。”
