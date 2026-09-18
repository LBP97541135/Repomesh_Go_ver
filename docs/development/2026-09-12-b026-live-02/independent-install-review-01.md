# B02.6 安装、权限和分页证据复核 01

复核时间：2026-09-12。范围限于 `docs/development/2026-09-12-b026-live-02/` 中的私有样本发现、分页、权限减少和恢复请求证据。本记录未读取秘密正文，未操作浏览器、服务或数据库。它不评价其他 LIVE 场景，也不把 B02.6 或 B02 标为 VERIFIED。

## LIVE-06

LIVE-06 标为 PASS 有证据支持。

`private-discovery-allowed.json` 记录实际工作区重试后得到 200。专用私有样本 `repo_00000000001367444901` 的 `userParticipation` 为 `allowed`，`appCapability` 也为 `allowed`。该文件将样本 ID 对应到 GitHub 安装设置中唯一选中的 repository ID `1367444901`，安装为 selected-repositories 范围；14 个返回项目中只有该样本的 App capability 为 allowed。`private-discovery-allowed-events.jsonl` 记录此次先收到 `RESULT_UNCONFIRMED` 503，再收到 200 的正常重试流程。

权限减少前的设置记录列出 Metadata read、Contents write 和 Pull requests write。随后允许观察发生在该专用样本上，支持 LIVE-06 所需的用户可读、安装范围和三项工作能力条件。该观察的 partial coverage 和 13 个安装外项目的 denied 状态符合 selected-repositories 安装范围，不能误写成完整仓库覆盖。

## 分页观察

`pagination-01.jsonl` 记录同源 API 使用 `limit=5` 的三页成功响应，分别有 5、5、4 个项目。汇总为 14 个唯一 ID、零重复 ID、专用样本出现，末页 `hasNext=false`，coverage 保持 `partial/APP_INSTALLATION_SCOPE`。首个 503 是 `RESULT_UNCONFIRMED`，随后第一页重试成功。

`pagination-01.mjs` 表明该结果来自浏览器当前同源会话中的 API fetch，脚本保存了经过字段白名单筛选的输出。它不证明页面 UI 点击过 50 项分页按钮，也不应作出这种声明。

## LIVE-07

LIVE-07 必须保持 IN_PROGRESS。

`permission-reduction-settings.json` 记录将专用 App 的 Pull requests 权限从 write 降为 read，GitHub 保存后安装页显示只剩 Pull requests read。随后 `private-permission-denied.json` 记录同一专用私有样本在实际 UI 重试后返回 `userParticipation=allowed` 与 `appCapability=denied/APP_PERMISSION_MISSING`。这是真实的、用户仍可读取的权限缺失样本，不是以私仓缺席代替 denied。相关事件记录同样显示 503 后的 200 重试。

`permission-restoration-request.json` 记录 App 已重新请求原有的 Contents write、Metadata read、Pull requests write，并且 GitHub 已保存请求；但安装授予的权限仍是减少后的状态，账户持有人接受新权限仍 pending。因此“恢复后观察 allowed”的 LIVE-07 必需条件尚未实现，不能填 PASS。

## 范围外状态

本复核不评价错误账号重连、失权样本、自然刷新、注销或移动视口。它们仍需独立真实证据，不能由本次允许、拒绝或分页观察推定完成。
