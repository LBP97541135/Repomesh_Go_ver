# B026 刷新观察工具独立复核 01

复核时间：2026-09-12。范围仅限 refresh-02 相对 refresh-01 的工具修复；全程只读，未启动 observer，未执行 SQL 快照，未操作浏览器、服务或 live 数据库。

## 结论

修复范围符合预期，解决了 refresh-01 已记录的 `INVALID_TIMESTAMP` 原因：Python 3.10 的 `datetime.fromisoformat` 不能接受 PostgreSQL JSON 可能输出的部分 1–5 位小数秒格式。新版改用标准 `strptime`，分别接受带 1–6 位小数秒和不带小数秒、并要求时区的 ISO 时间；`Z` 和数值时区均由单元测试覆盖。

这不是自然刷新、Cookie 或外部验收通过证据。refresh-02 尚未开始观察，本报告也不把工具测试扩大为 LIVE09 PASS。

## 差异核对

- `RUN_DIR_UNC` 从 refresh-01 改至 refresh-02，使之后可能的 Windows 后刷新调用只写新轮目录。
- `parse_time` 将 `fromisoformat` 改为两种明确格式：`%Y-%m-%dT%H:%M:%S.%f%z` 和 `%Y-%m-%dT%H:%M:%S%z`。它仍拒绝无时区值，并统一转换 UTC。
- `wait_until` 改为每轮计算一个 `remaining`，到期立即返回；睡眠继续限制为最多一秒。此改动不改变等待时长、停止文件语义或刷新分类条件。
- `snapshot.sql` 字节完全相同（SHA-256 `7819c8b2…514680b`），`post-refresh.mjs` 字节完全相同（SHA-256 `cfe424a7…d4fe436`）。未见查询、期限、身份、attempt、浏览器动作或成功判定的改动。

## 失败原因与测试证据

`refresh-01/real-timestamp-format-probe.json` 是只读的真实 PostgreSQL `clock_timestamp()` JSON 格式探针：100 个样本中有 11 个被原解析器拒绝，首个记录值为 `2026-09-12T10:05:40.81552-07:00`。这与 refresh-01 首样本后 `FAILED / INVALID_TIMESTAMP` 的现象相符；原始失败样本未捕获，因此该探针说明格式类别，不声称复现了同一个值。

`parser-test-red.txt` 记录了未修复副本对合法 1、2、4、5 位小数秒（含 `Z` 和 `+05:30`）的失败。`parser-test-green.txt` 记录修复后的通过结果。独立执行：

```text
python3 -m unittest -v docs/development/2026-09-12-b026-refresh-02/test_observer.py

Ran 2 tests in 0.006s
OK
```

测试遍历 0–6 位小数秒、`Z` 与 `+05:30`，并确认无时区输入仍被拒绝。它不连接数据库，也不启动 observer。

## 未改变的验收门槛

在新轮开始前，需以只读快照证明沿用的 actor、epoch 5、revision、未撤销 generation 5 和 baseline attempt，且没有新增 attempt。启动后至少取得三条连续正常样本，才可把“进程已启动”提升为“观察持续稳定”。此后仍须等到 `refresh_due_at`，以 epoch 与 revision 的自然单次推进、凭据期限推进、无人工 attempt 和后刷新浏览器结果共同判定；此前的 01 失败证据必须保留，不能覆写。
