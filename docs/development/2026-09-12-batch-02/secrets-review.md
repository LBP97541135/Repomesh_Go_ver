独立只读复核，2026-09-12。

复核者未参与秘密存储实现。范围为 [B02 采用记录](/home/xubohan/projects/Repomesh_Go_ver/docs/current/b02-authentication-adoption.md)、[来源稿 §2](/home/xubohan/projects/Repomesh_Go_ver/docs/current/backend-first-batch-sources-draft.md:31)、`/tmp/repomesh-b02-secrets/internal/secrets` 全部实现和测试，以及 migration `0002_auth_secrets.sql`。没有修改代码、启动外部服务或重跑测试。下列结论来自源码及 SQL 时序，已有测试名称只表示已检查其断言，不代替本次运行证据。

未发现 P0/P1。发现一项 P2，需要在根轮换验收前修正。

1. [P2] 在检查引用之前锁定当前包装根，避免被拒绝的并发配置切走有效根。

   [roots.go:44](/tmp/repomesh-b02-secrets/internal/secrets/roots.go:44) 读取当前版本引用，[roots.go:63](/tmp/repomesh-b02-secrets/internal/secrets/roots.go:63) 读取并验证自检，直到 [roots.go:84](/tmp/repomesh-b02-secrets/internal/secrets/roots.go:84) 才锁住旧 active root 并切换。`registerRoots` 的 advisory lock 只被其他 `registerRoots` 使用；`Seal`、`Rewrap` 和 `check` 的持久写入只通过 `requireActive` 取得根行共享锁。因而这些引用检查与随后取得根行写锁之间，可以出现新根引用。切换先提交，而 [store.go:67](/tmp/repomesh-b02-secrets/internal/secrets/store.go:67) 随后调用的 `check` 仍可能失败。

   可确定的交错如下。

   - A 已完成初始化，`self_check.root_id=A`。
   - B 以根集合 A+B 执行 `registerRoots`，激活 B 后暂停在 `check` 之前。A 只可解包，B 已是当前包装根。
   - C 以根集合 A+C 执行 `registerRoots`。C 的版本引用和自检检查此时都只要求 A。将 C 暂停在自检验证之后、旧 active root 更新之前。
   - B 执行 `check`，预扣 B 的包装次数，重包自检到 B，并通过 B 根行共享锁提交。B 初始化成功。
   - C 继续，将 B 标为退役并激活 C，提交注册事务。
   - C 的后续 `check` 读到 `self_check.root_id=B`，由于没有 B 文件而返回 `ErrConfiguration`。C 没有返回可用 Store；B 的后续 `Seal` 却已因 `active_wrap=false` 被拒绝。数据库仍保存 B 的引用，C 配置无法启动，B 配置又不能重新激活已经退役的 B。

   影响是并发部署或轮换中的可恢复服务中断，不是秘密正文泄漏或计数退款。修复应让切换时的引用检查观察到所有已授权旧根写入的最终结果，例如在 advisory lock 内、引用检查之前先获取当前 active root 的排他行锁，以等待已经持有共享锁的写入提交，并阻止后续写入通过 `requireActive`。随后再验证所需根和自检，最后切换。失败配置应在切换事务提交前被拒绝。需要同时保留包装预扣的独立提交，不能将计数合并进可回滚的切换事务。

   可补充同包 PostgreSQL 回归测试。先单独执行 B 的 `registerRoots`，再让 C 使用带 `pgx.QueryTracer` 的专用连接池，在 C 自检读取结束时用 channel 暂停；运行 B 的 `check` 后恢复 C。断言 C 被拒绝且 B 仍可包装，或经明确完整根集合接续后 C 成功。也可将旧根行锁屏障作为测试同步点。此场景尚未执行，现有 `TestPostgresSecretLifecycleAndRotation` 只覆盖顺序轮换，`TestPostgresSecretConcurrentWrapLimit` 只覆盖同一 active root 的并发包装。

其余检查结果如下。

| 检查项 | 源码结论及实际边界 |
| --- | --- |
| 权威 AAD | `Open` 从数据库读取 version、owner、purpose、format、root，先比较调用方期望，再由读取的 envelope 构造 AAD。长度编码包含格式、版本、ownerKind、ownerId、purpose，包装层另包含 rootID。没有使用调用方 owner 替换数据库 owner。`TestPostgresSecretAuthoritativeAAD` 覆盖数据库 owner/purpose/root 修改和密文篡改。 |
| 根指纹与单活 | SHA-256 指纹登记不可沿同 ID 换正文；数据库指纹唯一约束阻止将相同根换 ID 重置计数。advisory lock 和 partial unique index 阻止两个 active 行。已退役根不会因旧进程重启重新激活。上述 P2 是引用检查与引用写入之间的时序缺口。 |
| 包装预算 | `reserveWrap` 以独立事务原子递增，明确使用 `synchronous_commit=on`，成功提交后才调用 GCM。达到一百万次拒绝。Seal、首次自检、自检重包和普通重包均经过此路径，业务回滚和重包失败不退款。测试覆盖同根并发限额、业务插入失败、损坏包装导致重包失败及重启保留计数。 |
| nonce 和密钥 | AES-256-GCM 使用 `NewGCMWithRandomNonce`。每次正文密封生成独立 32 字节 DEK，正文仅密封一次；重包只更换 DEK 包装。迁移要求 wrapped DEK 为 60 字节，正文至少 28 字节，覆盖 nonce 和认证标签空间。密文认证失败不返回部分明文。 |
| Destroy | Destroy 和 SetEnabled 都先锁 availability。Destroy 清除正文及包装，留下 destroyed_at，增加 wrap_revision 和 access_epoch。SetEnabled 在锁后重新读取墓碑，不能重新启用。Open 同时要求 enabled 和未销毁；Rewrap 的 CAS 包含 destroyed_at IS NULL 和旧 revision，不能将销毁前读到的包装写回来。正常 API、并发启用和重启范围内没有发现复活路径。 |
| 轮换重试 | Rewrap 先验证旧包装与原正文，再用新根包装原 DEK，保持业务版本和正文不变。更新比较旧 rootID/revision，过期计算不会覆盖后续结果；每次重试都会先预扣，允许保守多计。未完成引用要求旧根仍在启动配置中，全部完成后可省略旧根。 |
| 根文件 | Unix 路径必须绝对，末级符号链接、非普通文件、非当前 effective UID、非精确 0600、非 32 字节全部拒绝。读取有长度上限，错误不含文件路径。非 Unix 实现明确拒绝，未实现 Windows ACL 读取支持。 |

需要保留的交付限制如下，这些不是上述 P2 的替代修复。

- 数据库备份回滚会同时回滚 wrap_count、墓碑和根激活历史。包内没有数据库之外的单调计数或恢复探测，不能宣称已自动防止灾备回滚重用旧包装额度。采用稿的责任是部署恢复时先停用旧发送实例，配置全新包装根，保留旧根只解包，并恢复未知外部操作责任。应在实际运维说明和恢复演练中落实。
- `retired_at` 当前在停止旧根包装时立即写入，旧根仍可且可能必须解包。该值不能解释为“已完成全部重包、验证并可删除旧根文件”。删除文件前仍需核全部未销毁版本及 self_check 引用，并遵守历史备份保留责任。
- 文件位于仓库及发布目录之外、服务账号与主机执行进程的根分发隔离，由部署路径和进程权限承担；此包只校验文件自身，不证明部署目录和服务账号已经正确隔离。
- Destroy 不承诺物理擦除 PostgreSQL 旧页、WAL、备份或已交付给调用方的明文。数据库恢复不能自动保留之后发生的销毁事实。

源码快照摘要如下。后续实现者修改文件后，本次结论须按变更范围重新核对。

```text
crypto.go                    4d156e83fe48b14fef1babf94c8e70e342848ef7d2895903c511cb9d5cfda094
roots.go                     a2d809248ba56dc8cb68771fdb0f4489baa412416730c0c2c8f0c8c149033af1
store.go                     6cb2a786093309370efc5a10f6fd83fd8d0edff84efc65de2df68c917ad89a5f
root_unix.go                 c7208a4efa5be840ce6864723d741f511bcae0943196f456bc687601410a8b90
root_other.go                07a3805b9a5236e11055768b4c12bacfdd3d4c2c2854e453e1343a038ac03235
0002_auth_secrets.sql         9b3cd90b34db8d76b5d753e3049a9a3cf1bd529a059150411f3aac0a39b13c72
```
