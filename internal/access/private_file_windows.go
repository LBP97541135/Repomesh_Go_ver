//go:build windows

package access

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Windows 无 Unix 的 0600/属主语义可断言：保持核心契约——绝对路径、常规文件、
// 上限 64KiB；目录与文件的 ACL 收敛（仅当前用户可读）由部署方保证。
// 生产部署目标为 Linux，属主与权限的严格检查在 linux 构建里不受影响。
func readPrivateFile(path string) ([]byte, error) {
	invalid := errors.New("App credential file must be an absolute, regular file no larger than 64KiB")
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, invalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, invalid
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, invalid
	}
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return nil, invalid
	}
	return data, nil
}
