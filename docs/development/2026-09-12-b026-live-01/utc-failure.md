# 非 UTC 环境下的真实响应被前端拒绝

## 已观察事实

第二次通过页面明确发起新登录，attempt ID 为 `6413121b-1b3c-42a7-8cee-34b8d32cc9d5`。2026-09-12 13:58:16.285 UTC 的真实 start 响应为 HTTP 201；浏览器在 30 秒内未跳转 GitHub。

主代理随后逐页核对专用浏览器，发现它有两个 RepoMesh 页面：

| 页面索引 | 当前路径 | 可见标题 |
| --- | --- | --- |
| 0 | /auth/result/6413121b-1b3c-42a7-8cee-34b8d32cc9d5 | 授权发起结果待确认 |
| 1 | /auth/result/00b02a0f-7ce4-4fda-8e6f-30e2240f1ff3 | 暂时无法读取授权结果 |

读取旧 attempt 的真实接口响应，仅提取非秘密字段，结果如下：

```json
{
  "httpStatus": 200,
  "attemptId": "00b02a0f-7ce4-4fda-8e6f-30e2240f1ff3",
  "state": "superseded",
  "reasonCode": "NEWER_ATTEMPT",
  "observedAt": "2026-09-12T06:58:16.237684-07:00",
  "observedAtUTC": false
}
```

`web/src/api.ts` 的 timestamp 校验只允许 UTC `Z` 格式，真实 observedAt 的 `-07:00` 被拒绝。当前 pgx timestamptz codec 在 ScanLocation 为空时使用 time.Local；startResult 也直接输出数据库扫描所得 expires。前者是实际接口证据，后者是源码定位；start 响应的原始 expiresAt 没有保存，不能称已直接捕获该字段。

## 纠正诊断结论

早期诊断把 page 0 的新尝试和 page 1 的旧尝试混为同页回退，并将仅校验 Date.parse 的自写检查称为与 parseStart 等价。该检查遗漏了前端的 UTC 格式约束。不能据此排除 INVALID_RESPONSE，或断言 session generation 是本次根因。旧日志保留，后续使用明确的页面索引和真实解析约束。

## 修复方向及验收状态

由数据库连接边界统一把 timestamptz 扫描结果表示为 UTC，保留原时刻和数据库数据，不放宽前端契约、不修改实际进程时区或数据库期限。新增非 UTC 测试验证后，构建新的 r2 配套发布包；旧 r1 和两次失败记录保留。

LIVE-01 入口检查通过；LIVE-02 和 LIVE-03 尚未完成真实取消或授权，后续因该缺陷暂时 BLOCKED。尚无登录会话或 GitHub 连接，未开始自然刷新计时。B02 整体仍为 IN_PROGRESS，B03 TODO。
