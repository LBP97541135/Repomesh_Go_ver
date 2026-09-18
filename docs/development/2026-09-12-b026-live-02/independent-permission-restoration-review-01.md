# LIVE-07 权限恢复独立复核 01

复核时间：2026-09-12。范围限于权限减少、同一私有样本 denied、恢复后 allowed、对应事件、第二账号阻断说明和 README 的 LIVE-07/LIVE-08 边界。未读取秘密正文，未操作数据库、浏览器或服务。

## 结论

LIVE-07 标为 PASS 有证据支持。

`permission-reduction-settings.json` 记录将专用 App 的 Pull requests 从 write 降为 read，并且 GitHub 已保存。`private-permission-denied.json` 随后记录相同专用私有样本仍为 `userParticipation=allowed`，但 `appCapability=denied/APP_PERMISSION_MISSING`。这证明了可读仓库上的真实 App 权限不足，未将私仓缺席伪称为 denied。

账号持有人批准恢复请求后，`private-permission-restored.json` 显示安装不再等待批准，权限文本回到 Metadata read、Contents write、Pull requests write。对同一样本的实际 RepoMesh 搜索和重试返回 200，用户参与和 App capability 均为 allowed，观察时间分别为 2026-09-12T16:44:00.065761858Z 和 2026-09-12T16:44:00.674697697Z。事件文件的 16:44:00.526Z `/api/repositories` 200 与该恢复观察相符。partial coverage 仍为 `APP_INSTALLATION_SCOPE`，没有被误记为完整覆盖。

因此，README 对 LIVE-07 的 PASS 与这条 denied 到 restored allowed 的连续证据一致。

## LIVE-08 和第二账号边界

`second-account-blocker.md` 清楚限定了尚未覆盖的两项：错误账号重连，以及撤回用户读权、等待原观察超过 60 秒后再次查询。当前账号拥有专用仓库，减少 App 权限或移出安装范围都不能代替用户失去读权。

README 对 LIVE-08 保持 BLOCKED，同时只保留已完成的私有发现、API `limit=5` 的三页 14 项无重复结果和安装范围外样本观察。它没有把 API 分页说成页面默认 50 项的下一页点击，也没有把当前账号的权限操作替代第二账号场景。该边界正确。

本复核不推定 LIVE-05、LIVE-08 的 blocked 子场景、LIVE-09、LIVE-10、B02.6 或 B02 已完成。
