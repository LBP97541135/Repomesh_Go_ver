//go:build unix

package main

import (
	"errors"
	"syscall"
)

// processAlive 判断一个 pid 现在是否还存在。
//
// 用 kill(pid, 0)：信号 0 不投递任何东西，只做存在性与权限检查。
//   - nil        → 进程在（且我们有权限给它发信号）
//   - EPERM      → 进程在，但不属于我们（同样是"在"）
//   - ESRCH      → 进程不存在
//
// 保守取向：任何说不清的返回都按"活着"处理。宁可漏收一条孤儿 run，
// 也不能把别人正在跑的活判死 —— 判死的代价是任务被重派、产出被丢弃。
func processAlive(pid int64) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(int(pid), 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}
