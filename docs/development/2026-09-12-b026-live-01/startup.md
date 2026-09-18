# 专用服务启动结果

实际报告时间：2026-09-12 06:52:01 PDT。配置与启动执行者：gpt-5.6 sol。

- 配置：`/home/xubohan/.config/repomesh/auth.json`，当前用户所有，0600，O_EXCL 排他创建。
- 数据库：`schema status=current current=3 target=3 pending=0`。
- Web：PID 177022，UID 1000，r1 包 `bin/repomesh-web --addr 127.0.0.1:8443 --assets <r1>/web/dist --auth-config /home/xubohan/.config/repomesh/auth.json`。
- Coordinator：PID 177023，UID 1000，同一 r1 包 `bin/repomesh-coordinator --auth-config /home/xubohan/.config/repomesh/auth.json`。
- 私有日志：`/home/xubohan/.local/state/repomesh-b026/web-20260912T065119-0700.log` 与 `coordinator-20260912T065119-0700.log`，均为 0600。
- 本机实际 TLS 校验通过，`ssl_verify_result=0`；没有禁用证书校验。
- `/healthz`：HTTP 200，status=scaffold。
- `/readyz`：HTTP 503，status=not_implemented，这是当前实现的预期结果。
- 未登录 `/api/session`：HTTP 401，AUTH_REQUIRED。
- `/login`：HTTP 200。

配置者原始脱敏报告保留在 `/tmp/repomesh-b026-live-redacted-909WHu.txt`，私有探测临时文件位于 `/home/xubohan/.local/state/repomesh-b026/probe.nUW1GS`。认证环境文件及秘密正文未复制到本目录。服务保持运行供本次真实验收使用；未清理共享资源。

本记录证明实际启动与 HTTP/TLS 探测；浏览器授权、安装能力和自然刷新需分别完成。
