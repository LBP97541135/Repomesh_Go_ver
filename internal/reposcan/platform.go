// Package reposcan implements the repository scan fetch layer: platform
// detection, the scannable-host guard, and platform fetchers that supply
// file trees, recent commits and file contents to the scan pipeline.
//
// Ownership note: this package is the scan track's exclusive edit area
// (decision D5/D8: Go-native scan engine). Nothing here imports business
// modules; the pipeline (channels, registration, graph) consumes the
// Fetcher interface defined below.
package reposcan

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Platform identifies a code-hosting platform a scan target can live on.
type Platform int

const (
	PlatformUnknown Platform = iota
	PlatformGitHub
	PlatformGitLab
	PlatformLocal
	PlatformUnsupported
)

// String renders the platform the way API surfaces and logs name it.
func (p Platform) String() string {
	switch p {
	case PlatformGitHub:
		return "github"
	case PlatformGitLab:
		return "gitlab"
	case PlatformLocal:
		return "local"
	case PlatformUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// Errors returned by Detect and Guard. Callers translate them into
// request rejections; the wording is user-facing on purpose.
var (
	ErrLocalPath       = errors.New("扫描目标不能是本地路径，请提供 GitHub/GitLab 仓库地址")
	ErrUnknownPlatform = errors.New("无法识别的代码托管平台：请在平台映射中声明该主机")
	ErrHostDenied      = errors.New("该主机不在允许扫描的名单中")
)

// UnsupportedError names a well-known platform the scanner does not serve.
type UnsupportedError struct{ Host string }

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s 暂不支持仓库扫描，当前仅支持 GitHub 与 GitLab", e.Host)
}

// wellKnownUnsupported lists hosting services recognised well enough to be
// named in a refusal instead of a generic "unknown platform".
var wellKnownUnsupported = map[string]bool{
	"bitbucket.org":   true,
	"gitee.com":       true,
	"sourceforge.net": true,
}

// DetectPlatform classifies a pasted repository or group URL.
//
// Local paths (drive letters, "./") are reported as PlatformLocal: the
// scanner only serves remote hosts, and naming the shape honestly lets the
// caller refuse with a clear message instead of guessing.
//
// extra maps declared hosts (lowercase, e.g. "git.example.com") to
// "github" or "gitlab", for self-hosted instances; declarations win over
// the built-in table.
//
// The returned host is the lowercase hostname ("" for local paths).
func DetectPlatform(rawURL string, extra map[string]string) (Platform, string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return PlatformUnknown, "", fmt.Errorf("扫描地址不能为空")
	}
	if isLocalPath(trimmed) {
		return PlatformLocal, "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return PlatformUnknown, "", fmt.Errorf("无法识别的扫描地址：%s", trimmed)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return PlatformUnknown, "", fmt.Errorf("无法识别的扫描地址：%s", trimmed)
	}
	switch kind := strings.ToLower(extra[host]); kind {
	case "github":
		return PlatformGitHub, host, nil
	case "gitlab":
		return PlatformGitLab, host, nil
	case "":
		// fall through to the built-in table
	default:
		return PlatformUnknown, host, fmt.Errorf("平台映射声明无效：%s=%s（仅支持 github/gitlab）", host, kind)
	}
	switch host {
	case "github.com", "www.github.com":
		return PlatformGitHub, host, nil
	case "gitlab.com", "www.gitlab.com":
		return PlatformGitLab, host, nil
	}
	if wellKnownUnsupported[host] {
		return PlatformUnsupported, host, &UnsupportedError{Host: host}
	}
	return PlatformUnknown, host, nil
}

// Guard verifies that rawURL names a scannable remote target on an
// allowlisted host, and returns the platform plus the normalized URL
// (trimmed, trailing slashes and a trailing ".git" removed).
//
// Every scan entry must pass through Guard before any outbound request:
// the refusals that must happen before egress cannot be forgotten by the
// next entry point someone adds.
func Guard(rawURL string, allowlist []string, extra map[string]string) (Platform, string, error) {
	platform, host, err := DetectPlatform(rawURL, extra)
	if err != nil {
		return platform, "", err
	}
	if platform == PlatformLocal {
		return PlatformLocal, "", ErrLocalPath
	}
	normalized := normalizeRepoURL(rawURL)
	denied := true
	for _, allowed := range allowlist {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			denied = false
			break
		}
	}
	if denied {
		return platform, "", ErrHostDenied
	}
	return platform, normalized, nil
}

// isLocalPath reports whether the target is a filesystem path (a drive
// letter like "D:\repo" or a relative "./repo") rather than a URL.
func isLocalPath(value string) bool {
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, ".\\") {
		return true
	}
	if len(value) >= 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return false
}

// normalizeRepoURL trims whitespace, surrounding slashes and a trailing
// ".git" so the same repository spelled two ways scans as one target.
func normalizeRepoURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	trimmed = strings.TrimRight(trimmed, "/")
	return strings.TrimSuffix(trimmed, ".git")
}

// SplitRepoPath extracts the repository path segments from a normalized
// URL: "owner/repo" for GitHub (two segments), the full namespace path for
// GitLab (every segment). The boolean is false when the URL does not name
// at least one segment below the host.
func SplitRepoPath(normalizedURL string) ([]string, bool) {
	parsed, err := url.Parse(normalizedURL)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			segments = append(segments, part)
		}
	}
	if len(segments) == 0 {
		return nil, false
	}
	return segments, true
}

// NormalizeGroupURL trims whitespace, trailing slashes and a trailing
// ".git" from a group/org address (same normalization as repositories).
func NormalizeGroupURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	trimmed = strings.TrimRight(trimmed, "/")
	return strings.TrimSuffix(trimmed, ".git")
}
