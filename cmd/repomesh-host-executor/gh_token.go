package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/github"
)

// authFile is the subset of the deployment auth.json needed to sign an App JWT.
type authFile struct {
	AppID            string `json:"appId"`
	ClientID         string `json:"clientId"`
	CallbackURL      string `json:"callbackUrl"`
	ClientSecretFile string `json:"clientSecretFile"`
	PrivateKeyFile   string `json:"privateKeyFile"`
}

// mintInstallationToken mints one installation access token for owner/name
// using the deployment's own GitHub App identity.
//
// 2026-09-19 C 修复：交付不再读部署级缓存文件（.gh-token 是写死单个仓库刷新
// 的，别人把 App 装到自己账号上也推不动）。改为**按仓库现场铸**：installation
// token 天然限定在"该仓库所属 installation 勾选的那批仓库"内，所以任何账号或
// 组织只要装了 App，它的仓库就能被交付，无需任何部署级钉死。
//
// 令牌只在内存里活到子进程环境变量为止：不落盘、不进命令台账。
func mintInstallationToken(ctx context.Context, repoFullName string) (string, error) {
	owner, name, ok := strings.Cut(repoFullName, "/")
	if !ok || owner == "" || name == "" || strings.ContainsAny(repoFullName, " \t\n") {
		return "", fmt.Errorf("repository must be owner/name")
	}
	authPath := os.Getenv("REPOMESH_AUTH_CONFIG")
	if authPath == "" {
		authPath = "/etc/repomesh/auth.json"
	}
	raw, err := os.ReadFile(authPath)
	if err != nil {
		return "", fmt.Errorf("read auth config: %w", err)
	}
	var auth authFile
	if err := json.Unmarshal(raw, &auth); err != nil {
		return "", fmt.Errorf("decode auth config: %w", err)
	}
	secret, private := auth.ClientSecretFile, auth.PrivateKeyFile
	client, err := github.New(github.Config{
		ClientID:     auth.ClientID,
		AppID:        auth.AppID,
		CallbackURL:  auth.CallbackURL,
		ClientSecret: func(context.Context) ([]byte, error) { return os.ReadFile(secret) },
		PrivateKey:   func(context.Context) ([]byte, error) { return os.ReadFile(private) },
	})
	if err != nil {
		return "", fmt.Errorf("github client: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	token, _, err := client.InstallationAccessToken(ctx, owner, name)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("empty installation token")
	}
	return token, nil
}
