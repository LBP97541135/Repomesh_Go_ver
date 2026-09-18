# r2 → r3 专用服务切换计划

本计划只切换 B02.6 专用 Web 与 coordinator。它不迁移数据库，不停止 PostgreSQL，不操作 Windows Chromium，也不接触其他服务。切换脚本为私有文件 `/tmp/repomesh-b02-live/switch-r3.py`；本轮只准备和审阅脚本，不执行。

## 固定对象

- 旧版本：`dist/repomesh-0.2.0-b026-utc-20260912-r2`
- 新版本：`dist/repomesh-0.2.0-b026-cookie-20260912-r3`
- 旧 Web：PID 192940、UID 1000
- 旧 coordinator：PID 192941、UID 1000
- 私密运行环境：`/home/xubohan/.local/state/repomesh-b026/runtime.env`
- 新证据：`docs/development/2026-09-12-b026-live-03/switch.json`

脚本会从 `/proc` 重新读取两个 PID 的 UID、可执行文件和完整参数，并要求它们逐项等于当前 r2 进程。任何 PID 复用、参数变化、路径变化或进程缺失都会在发送信号前失败。

## 执行顺序

1. 独占预留 `switch.json`，拒绝覆盖已有证据。
2. 读取 r3 `release.json`，要求版本与 Linux AMD64 目标正确，清单中的全部文件存在且 SHA-256 匹配，并拒绝清单外混入文件。
3. 分别运行三个 r3 入口的 `--version`，要求精确报告 r3 版本。
4. 核对两个旧进程的固定 PID、UID、可执行文件和非秘密参数。
5. 核对 `runtime.env` 是 UID 1000 拥有的普通 0600 文件。严格解析唯一的 `export REPOMESH_RELEASE`，要求其仍指向 r2；其他运行变量只用于受控子进程环境，不写入证据或终端。
6. 用 r3 Web 执行只读 `db check`，要求精确得到 `current=3 target=3 pending=0`。脚本没有迁移调用。
7. 以 0600、独占方式保存 `runtime.env` 备份，只替换唯一 release 值，保留其余字节。
8. 再次核对旧进程身份，仅向这两个 PID 发送 `SIGTERM`。最多等待 10 秒；超时即失败，不发送 `SIGKILL`。
9. 使用更新后的受控环境和原进程参数启动 r3 Web 与 coordinator。Web 的 assets 路径随 release 更新。两个进程各写一份新的私密 0600 日志，并以独立 session 脱离脚本。
10. 等待两秒，确认两个新进程仍存活、UID 和可执行文件均正确，再写完成证据。

## 失败与证据边界

脚本只向终端打印 `SWITCH_COMPLETE` 或 `SWITCH_FAILED`，详细结果使用固定错误码写入 `switch.json`。证据包含时间、版本、发布清单和 artifact 哈希、旧新 PID、私密日志路径、运行环境备份路径及数据库核查结果；不含环境正文、数据库连接串、秘密、token 或原始日志和异常文本。

更新环境后发生失败时，脚本保留独占备份和失败结果并停止，不自动回滚或重启 r2。已发送 `SIGTERM` 后也不会扩大终止范围；这类失败需要先按证据和私密日志人工核对，再决定恢复动作。

审批后从仓库根目录执行：

```bash
python3 /tmp/repomesh-b02-live/switch-r3.py
```

成功只证明专用服务已从 r2 切换到 r3，且启动前数据库 schema 为 3/3/0；不代表真实 GitHub 回归、自然刷新或 B02 全批通过。
