//go:build linux || darwin

package server

import (
	"fmt"
	"os"
	"syscall"

	"my-vpn/internal/system"
)

// flock 由内核在进程退出后释放，避免遗留锁阻塞后续启动。锁文件本身不删除，防止 inode 竞态。
func acquireLock(path string) (func(), error) {
	if err := system.SafePath(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another my-vpn operation is running: %w", err)
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}
