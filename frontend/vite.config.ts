import fs from "node:fs";
import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const pkg = JSON.parse(
  fs.readFileSync(path.resolve(import.meta.dirname, "package.json"), "utf8"),
) as { version?: string };

export default defineConfig(() => {
  return {
    plugins: [react(), tailwindcss()],
    define: {
      __APP_VERSION__: JSON.stringify(pkg.version ?? "0.0.0"),
    },
    server: {
      host: "127.0.0.1",
      port: 5280,
      strictPort: true,
      // 联调：同源代理到 Go 后端（repomesh-web，默认 127.0.0.1:8080）。
      // 会话是 httpOnly 的 __Host- cookie——必须 localhost 访问且同源代理，
      // 跨源要 CORS + credentials，同源代理下什么都不用配。
      // 目标可由 REPOMESH_API_TARGET 覆盖（如 Go 后端换了端口）。
      // secure:false —— 后端部署用自签证书（auth-config 的 TLS 段），不校验证书链。
      // headers.origin —— 后端写请求校验 Origin 与 auth-config 的 origin（https://
      // localhost:8080）严格相等；dev 源是 localhost:5280，须在这里覆写，否则登录
      // 等全部写操作 403 ORIGIN_REJECTED。
      proxy: {
        "/api": {
          target: process.env.REPOMESH_API_TARGET ?? "https://127.0.0.1:8080",
          changeOrigin: true,
          secure: false,
          headers: { origin: process.env.REPOMESH_API_ORIGIN ?? "https://localhost:8080" },
        },
      },
    },
  };
});
