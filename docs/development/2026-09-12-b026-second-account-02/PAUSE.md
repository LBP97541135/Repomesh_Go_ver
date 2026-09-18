# B02.6 第二账号验收 02 暂停交接

暂停于 2026-09-12 22:31 PDT。用户要求停止所有外部交互，保留当前成果。本文件是冷启动接续入口；[README](README.md)、[验收记录](live-acceptance.md)和[恢复清单](restore-01.json)提供细节。未 commit、未 push，全部现有修改保留；旧 `verify-record.py` 未运行。

## 已确定状态

- LIVE-09、LIVE-10 已通过最终独立复核，不重跑。
- B 初始完整分页已观察到 B-only 仓库稳定 ID `1368000734`，A-owned 仓库稳定 ID `1367444901` 的 `userParticipation` 与 `appCapability` 均为 `allowed`。
- 本轮 LIVE-05 为 FAIL，LIVE-08 为 NOT_RUN。A 同账号 reconnect 先 confirmed，12 秒后出现独立 B login 并改变 A 浏览器上下文；错误账号 reconnect 场景没有被覆盖，不能称为 `ACCOUNT_MISMATCH` 产品缺陷。
- A浏览器中的误建B RepoMesh会话和B浏览器原RepoMesh会话均已通过正常UI logout 204，随后 `/api/session` 为401。
- RESTORE_IN_PROGRESS。B02保持IN_PROGRESS；B03 worktree LOCAL_VERIFIED，主目录 integration NOT_RUN。

## 外部现状

- 专用GitHub App仍为PUBLIC。Make private因B installation存在被GitHub拒绝，尚未恢复为private。
- B installation `161284386` 仍存在，只选择仓库稳定 ID `1368000734`。
- B仍是A-owned仓库稳定 ID `1367444901` 的协作者；协作者与pending invitation缺席尚未恢复验证。
- B OAuth grant尚未撤销。
- A installation `161172403` 是否保持原范围与权限，以及A是否无法访问B仓库，仍待最终只读复核。
- GitHub profile A：CDP `9231`，当前账号 `catbobyman`，稳定 ID `137759882`，Chrome PID `50296`。
- GitHub profile B：CDP `9230`，当前账号 `bohanxu111`，稳定 ID `328458192`，Chrome PID `45576`。
- 两个CDP当前存活；页面target ID可能随导航变化，不得作为永久入口。
- r3 Web PID `287037`、coordinator PID `287038` 当前存活，固定origin为 `https://repomesh.bohanxu.me:8443`。非秘密auth config绝对路径为 `/home/xubohan/.config/repomesh/auth.json`；只把路径用于接续定位，不要读取或输出文件正文。cookie、token、OAuth state/code和其他秘密不写入正文。

## 严格恢复顺序

1. 在B profile卸载installation `161284386`，随后只读验证installation absent。
2. 在A profile将专用App改回private，随后只读验证private。
3. 移除B对A-owned仓库的协作者关系，并验证协作者与pending invitation均 absent。
4. 在B profile撤销该App的OAuth grant，并验证authorization absent。
5. 只读验证A installation `161172403` 仍只选择稳定 ID `1367444901`，权限为Metadata read、Contents write、Pull requests write。
6. 只读验证A无法访问B仓库稳定 ID `1368000734`。
7. 以新文件更新恢复证据，完成未参与主要操作的独立复核。只有全部项目完成才能把RESTORE标为PASS。

完整恢复后，新建 `docs/development/2026-09-12-b026-second-account-03/`，从头重跑LIVE-05与LIVE-08。不得覆盖本目录任何成功或失败证据。

## 关键证据

- [刷新最终独立复核](../2026-09-12-b026-refresh-02/independent-final-review-01.md)：LIVE-09、LIVE-10 PASS。
- [B初始分页](b-live-05-01.json)：SHA-256 `2195ba0560deb34e0badc720b2b248bbf54b6b88d19f28eb42759c87ee14327c`。
- [A基线](a-live-05-baseline-01.json)：SHA-256 `61af824ce0661eabb2521a1fd7d116fe0cfa5af7670acf9203001729b62e9496`。
- [LIVE-05失败](a-live-05-account-mismatch-01.json)：SHA-256 `bd61d43f377850de14a9357463597371cc9794240033f75451ffe23f818a31b5`。
- [失败时间线](a-live-05-failure-timeline.json)：SHA-256 `cfa00e00ba788915240b8b5e308b853a212a4bae5a93d6cb9dc13200db5fe68f`。
- [误建会话清理](a-accidental-b-session-cleanup-01.json)：SHA-256 `8c0674b7ce79ac4d42b00031844912e9ecc9292897a35c7e9fcd8bd0bed76a45`。
- [恢复清单](restore-01.json)：SHA-256 `037de3173ba20443541835fcf6d8edb47f8f4c9458c73af1bd39d2c5d8f506ab`。
- [B接受协作者邀请](b-collaborator-accepted.json)：SHA-256 `f9d7e8d62cd0cab2dc2e230f300bb6c3d7847b68fe0164447a192db529fe8981`。
