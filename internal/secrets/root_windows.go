//go:build windows

package secrets

import (
	"io"
	"os"
	"path/filepath"
)

// Windows 无 Unix 的 0600/属主语义可断言：保持核心契约——绝对路径、常规文件、
// 恰好 32 字节；目录与文件的 ACL 收敛（仅当前用户可读）由部署方保证。
// 生产部署目标为 Linux，属主与权限的严格检查在 unix 构建里不受影响。
func readRootFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrConfiguration
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrConfiguration
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrConfiguration
	}
	key, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, ErrConfiguration
	}
	return key, nil
}
