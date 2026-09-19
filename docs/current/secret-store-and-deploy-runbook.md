# 密钥存储与部署运维手册（含 2026-09-20 宕机事故复盘）

> 这份文档是 2026-09-20 一次**全站宕机 + 191 条密钥永久丢失**事故的直接产物。
> 事故本身不是代码 bug，而是"主密钥被换掉 + 回滚救不了"这个组合。这里把机制、
> 失败模式、恢复步骤与已落地的加固一次写清，避免下一个人重踩。

## 1. 密钥存储是怎么工作的

```
/etc/repomesh/auth.json          部署配置：activeRootId、roots[]（root id + key 文件路径）、
                                 App 凭据文件路径、origin、callback
/etc/repomesh/root-<id>.key      主密钥文件（32 字节，0600）
repomesh_secrets.root_keys       每把 root 的**指纹**（sha256(key)）、active_wrap、wrap_count
repomesh_secrets.versions        已封存密钥（信封加密）：purpose + 密文 + root_id
repomesh_secrets.self_check      自检串（用某把 root 封存，用来验证 root 可用）
```

启动时 `secrets.New()` 做三件事（`internal/secrets/store.go` / `roots.go`）：

1. 读 `roots[]` 里每个 key 文件，算指纹；
2. `registerRoots`：把指纹 INSERT 进 `root_keys`（冲突则不动），**然后比对库里的指纹** ——
   不一致直接 `ErrConfiguration`（"secret store configuration is invalid"）；
   另外，凡是 `versions` / `self_check` 里出现过、但**配置里没有**的 root_id，也报
   `ErrConfiguration`；
3. `check`：用 active root 验/换封 `self_check`，任一步读不到就 `ErrUnavailable`
   （"secret source is unavailable"）。

**关键性质：key 文件与库里指纹是一对绑死的。换了 key 文件而没同步库，服务拒绝启动。**

## 2. 失败模式（2026-09-20 实际发生）

| 时间 | 事件 |
|---|---|
| 05:37:22 | `/etc/repomesh/root-1.key` 被替换（**非部署流程所为**，是有人在服务器上手工操作） |
| 05:37:54 | web 启动自检 `ErrConfiguration` → systemd 崩溃循环（30 分钟内 30+ 次重启） |
| 05:39+ | 错误变为 `ErrUnavailable`，全站 502 |
| 05:4x | 手工救场：**销毁 191 条已封存密钥**，只留 4 条并用新 root 重新封存 |
| 05:52 | 服务能起来，但跑的是**手工 `dev` 构建**（无版本号，不可追溯） |

**永久损失**：43 个用户的 GitHub token/refresh token（需重新登录）、1 条模型中转站密钥
（需重新填写）。GitHub App 凭据走文件，未受影响。

**为什么回滚没救回来**：部署工作流的回滚**只回滚 bin/dist**，不碰 `/etc/repomesh`。
所以"环境侧坏掉"这类故障，回滚是无效的。

## 3. 已落地的加固

`.github/workflows/deploy.yml`：

- **备份**：每次部署把 `root-*.key` 与 `auth.json` 拷进 `$BK/secrets/`（备份目录 root-only，
  `cp -a` 保留 0600）；
- **回滚**：失败回滚时一并 `cp -a "$BK/secrets/." /etc/repomesh/` —— 有了这一条，
  上述事故会在"部署失败 → 自动回滚"里被止住，密钥不会丢；
- 顺带修：回滚取"最新备份"改用 `ls -dt backups/*/`（只匹配目录）——该目录下可能混着
  手工备份的 `.tgz`，不带尾斜杠时一旦它最新，回滚会去找不存在的 bin/dist。

## 4. 恢复手册

**情况 A：key 文件被换掉，但旧 key 还在（备份里或你手上有）**

```bash
BK=$(ls -dt /opt/repomesh/backups/*/ | head -1)
cp -a "$BK/secrets/root-1.key" /etc/repomesh/root-1.key
systemctl restart repomesh-web repomesh-coordinator repomesh-host-executor
# 验证：/healthz 返回 200，且 journalctl 无 "secret ..." 报错
```
**这是首选路径 —— 无损恢复，不需要任何人重新登录。**

**情况 B：旧 key 永久丢失**

只能换新 root，代价是**所有已封存密钥不可读**（用户重登、模型中转站密钥重填）：

```bash
# 1) 生成新 key（32 字节）
head -c 32 /dev/urandom > /etc/repomesh/root-1.key && chmod 600 /etc/repomesh/root-1.key
# 2) 让库侧与新 key 一致：旧密文已不可解，标记销毁；自检串删掉让它用新 root 重封
psql "$REPOMESH_DATABASE_URL" -c "UPDATE repomesh_secrets.versions SET destroyed_at=now() WHERE destroyed_at IS NULL"
psql "$REPOMESH_DATABASE_URL" -c "DELETE FROM repomesh_secrets.self_check"
# 3) 把 root_keys 里的旧指纹删掉 —— 启动时 registerRoots 会用新 key 的指纹重新插入
psql "$REPOMESH_DATABASE_URL" -c "DELETE FROM repomesh_secrets.root_keys WHERE root_id='root-1'"
systemctl restart repomesh-web repomesh-coordinator repomesh-host-executor
```

**不要**在没看清上面两步之前重启服务 —— 崩溃循环会让站点一直 502，而每次重启都不改变事实。

## 5. 排查清单（症状 → 先看什么）

| 症状 | 先看 |
|---|---|
| 全站 502、`systemctl is-active repomesh-web` 不是 active | `journalctl -u repomesh-web -n 50` |
| 日志有 `secret store configuration is invalid` | key 文件与 `root_keys.fingerprint` 是否一致（`sha256sum` vs 库） |
| 日志有 `secret source is unavailable` | `root_keys.active_wrap` 是否为 t；`versions`/`self_check` 是否引用了未配置的 root |
| `/healthz` 能通但版本是 `dev` | 有人手工构建过 —— 走一次正常 push 部署让它回到可追溯版本 |
| 部署后站点短暂 502 | 正常：部署会重启三进程，约 30 秒内恢复 |

## 6. 一条教训（写给多人协作的场景）

事故的触发动作（替换主密钥）**不是部署流程做的**，是有人在服务器上手工操作。
只要"多个人/多个自动化能同时改同一台服务器"，这类故障就会复发。
**加固只能降低损失，不能替代"谁有权改生产环境"这件事本身的管理。**