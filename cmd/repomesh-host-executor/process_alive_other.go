//go:build !unix

package main

// processAlive 在非 unix 平台上没有可靠的实现 —— 返回 true（"当作活着"）。
//
// 这是刻意的保守选择：收尾孤儿 run 的能力只在真正跑 agent 的宿主（Linux）上需要。
// 在没有实现的地方谎报"进程已死"，会把正在跑的活判死并触发重派，代价远大于漏收。
func processAlive(pid int64) bool { return true }
