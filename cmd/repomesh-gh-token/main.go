// repomesh-gh-token mints one GitHub App installation access token using the
// deployment auth.json (RepoMesh's own GitHub App identity) and prints it.
// The delivery pipeline clones/pushes/opens PRs as the App (repomesh-bot).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/github"
)

type authFile struct {
	AppID            string `json:"appId"`
	ClientID         string `json:"clientId"`
	CallbackURL      string `json:"callbackUrl"`
	ClientSecretFile string `json:"clientSecretFile"`
	PrivateKeyFile   string `json:"privateKeyFile"`
}

func main() { os.Exit(run()) }

func run() int {
	flags := flag.NewFlagSet("repomesh-gh-token", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	authPath := flags.String("auth-config", os.Getenv("REPOMESH_AUTH_CONFIG"), "authentication deployment JSON file")
	repo := flags.String("repo", "", "owner/name of one repository governed by the App installation")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if *authPath == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "usage: repomesh-gh-token -auth-config /etc/repomesh/auth.json -repo owner/name")
		return 2
	}
	raw, err := os.ReadFile(*authPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read auth config:", err)
		return 1
	}
	var auth authFile
	if err := json.Unmarshal(raw, &auth); err != nil {
		fmt.Fprintln(os.Stderr, "decode auth config:", err)
		return 1
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
		fmt.Fprintln(os.Stderr, "github client:", err)
		return 1
	}
	owner, name, ok := strings.Cut(*repo, "/")
	if !ok || owner == "" || name == "" {
		fmt.Fprintln(os.Stderr, "repo must be owner/name")
		return 2
	}
	token, expires, err := client.InstallationAccessToken(context.Background(), owner, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mint token:", err)
		return 1
	}
	fmt.Println(token)
	fmt.Fprintln(os.Stderr, "expires:", expires.Format(time.RFC3339))
	return 0
}
