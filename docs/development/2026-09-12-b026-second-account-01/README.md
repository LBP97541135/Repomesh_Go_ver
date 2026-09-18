# B02.6 第二账号验收 01

本目录保存 LIVE-05 错误账号重连与 LIVE-08 用户读权撤回的后续真实验收。测试账号 `bohanxu111` 已由账号持有人创建；2026-09-12 10:41 PDT，账号持有人确认邮箱验证完成。公开账号存在与邮箱验证确认只构成准备条件，不代表登录、App 授权、私仓访问或任一 RepoMesh 场景已经通过。

当前轮次为 `PREPARED_NOT_RUN`。为保护 [自然刷新观察 02](../2026-09-12-b026-refresh-02/README.md) 的 epoch 5 基线，在该观察写出真实刷新确认且后续浏览器结果通过前，不改变 GitHub App 可见性、安装范围或仓库协作者，不把第二账号加入当前专用 Chrome，也不发起 RepoMesh 登录或重连。

## 固定范围

| 项目 | 值或约束 |
| --- | --- |
| RepoMesh origin | `https://repomesh.bohanxu.me:8443` |
| GitHub App | `repomesh-bohan-b026`；专用验收 App |
| 账号 A | `catbobyman`；现有 RepoMesh 连接的 GitHub 账号 |
| 账号 B | `bohanxu111`；错误账号与读权撤回样本 |
| 仓库样本 | A 持有的安装内专用私仓 1367444901 用于读权撤回；另由 B 建立一个 A 不可见的专用私仓，用于跨账号不披露判据；证据正文不记录私仓名称 |
| 当前发布 | `repomesh-0.2.0-b026-cookie-20260912-r3` |
| 替身、时间修改、token 重放 | 均禁止 |
| 当前状态 | LIVE-05 `NOT_RUN`；LIVE-08 用户读权撤回 `NOT_RUN` |

## 执行门槛

下列条件必须同时成立后才能开始本目录的外部变更：

1. `../2026-09-12-b026-refresh-02/confirmed-refresh.json` 存在，且只读前后快照证明同一 actor 自然刷新；
2. `../2026-09-12-b026-refresh-02/browser-result.json` 存在并明确通过刷新后发现、桌面／移动视口、实际注销和历史往返；
3. 观察器已经正常退出，当前浏览器旧 RepoMesh 会话为 401；
4. 刷新期间没有新增登录、重连、App 配置或仓库权限变更；
5. 刷新与浏览器结果完成独立复核，或至少保持原始输出不再变化并在本轮结束前补齐独立复核。

任一条件不成立时保持 `PREPARED_NOT_RUN`，不以账号已创建、页面已打开或配置检查代替验收。

## 文件

- [PLAN.md](PLAN.md)：按状态边界执行的操作和回滚顺序。
- [live-acceptance.md](live-acceptance.md)：沿用现有模板记录本轮实际结果。
- [independent-preparation-review-01.md](independent-preparation-review-01.md)：准备方案的独立只读复核；没有开放 P0/P1/P2，不代表真实场景通过。

所有原始浏览器网络、Cookie、OAuth code/state、csrfToken、authorizationUrl、token、数据库连接串与私仓名称均不得写入本目录。稳定仓库 ID 可以进入证据。需要保存浏览器事件时只保留白名单字段；数据库查询只保存非秘密状态和时间字段。
