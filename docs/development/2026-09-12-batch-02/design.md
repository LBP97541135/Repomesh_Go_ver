# B02 实现形状

以方案 A 为基础。独立 cross-judge 使用 gpt-5.6-luna，按契约、并发、接口深度、验证和维护成本给 A 23 分、B 21 分。A 的浏览器双代次与独立外部适配边界保留，B 的故障用例和 HTTP 不拼事务约束合入。

主代理将认证与发现放在 `internal/access` 的不同职责文件，共同持有连接代次的事务边界；秘密和 GitHub 协议分别位于 `internal/secrets` 与 `internal/github`。入口使用 `database.DB.Pool()` 组装，Web 只映射 HTTP，coordinator 运行有限持久工作。原迁移测试固定 0001 夹具，新增产品迁移另验。

```text
Web HTTP -> access.Service -> PostgreSQL
                         -> secrets.Store -> PostgreSQL + 根文件
                         -> github.Client -> 固定 GitHub HTTPS API
coordinator -> access.Service.RunOne -> 同一连接及发现事务规则
```

核心记录为根注册、秘密版本、浏览器绑定、稳定账号、会话、登录尝试、GitHub 连接和发现批次。绑定分别保存身份代次与尝试代次。交换和刷新以持久唯一责任控制，未知后不重领；只读发现使用有期限 claim 并核新旧版本。

`secrets.Seal/Open` 使用命名 Owner、Purpose 和 VersionID；调用方限于已核验用例。未采用 callback 能强制清除 Go 明文的说法，闭包并不能阻止复制。秘密包装独立预扣额度，之后失败也不退。最终连接、会话及尝试在同一短事务引用已封装版本，确定未提交的密文及时销毁；提交确认未知或崩溃产生的无引用版本，超过15分钟写入窗口后按权威引用清理。不得因本地超时销毁可能已提交的版本。

GitHub 仓库复核使用公开的 owner/name 读取端点，结果还需与批次保存的稳定仓库 ID 相同。不依赖未公开的按数值 ID 读取端点。改名或转移无法确认时不披露旧名称，重新发现恢复。

当前项目、Issue、会话和模型原操作的授权模块尚未实现，对这些返回目标保留 unknown，结果页 nextPage 为 null，不制造已确认授权。根页面可在当前 session 确认后返回。

验证优先覆盖错误 owner/AAD、根轮换、计数失败不退、并发 start、旧回调与注销、refresh 未知、发现 claim 和重启。真实 GitHub App 仍未配置，本地适配器证据和外部验收分开。

后续复核补充了跨浏览器连接 fence、根预检前的活动根行锁、未交付完成批次的重读，以及结果页会话代次改变后停止旧组件查询。采用含义与实测见[结果记录](README.md)。
