package system

import (
	"fmt"
	"os"
	"path/filepath"
)

// SafePath 拒绝路径上已有的符号链接，避免 root 写入被重定向到其他文件。
func SafePath(path string) error {
	path = filepath.Clean(path)
	for path != "." && path != string(filepath.Separator) {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink: %s", path)
		}
		path = filepath.Dir(path)
	}
	return nil
}

// AtomicWrite 在目标目录创建临时文件，再通过 rename 提交，避免进程退出留下半份配置。
func AtomicWrite(path string, data []byte, mode os.FileMode, uid, gid int) error {
	if err := SafePath(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".my-vpn-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if uid >= 0 || gid >= 0 {
		if err := file.Chown(uid, gid); err != nil {
			return err
		}
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
