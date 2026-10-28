package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"my-vpn/internal/system"
)

// snapshot 保存原文件是否存在、内容和权限，恢复时可以区分覆盖与首次创建。
type snapshot struct {
	Path   string      `json:"path"`   // 原始绝对路径。
	Backup string      `json:"backup"` // 快照文件名，不包含用户提供的路径片段。
	Exists bool        `json:"exists"` // 原文件不存在时，回滚会删除此次新建的文件。
	Mode   os.FileMode `json:"mode"`   // 原始权限，恢复后保留。
	UID    int         `json:"uid"`    // 原文件属主，恢复时保留而不是假设为 root。
	GID    int         `json:"gid"`    // 原文件属组，恢复时保留配置读取权限。
}

type transaction struct {
	dir   string
	files []snapshot
}

func newTransaction(root string, paths []string) (*transaction, error) {
	dir, err := os.MkdirTemp(root, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return nil, err
	}
	tx := &transaction{dir: dir}
	for i, path := range paths {
		if err := system.SafePath(path); err != nil {
			return nil, err
		}
		item := snapshot{Path: path, Backup: fmt.Sprintf("%d.bak", i)}
		info, err := os.Stat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("not a regular file: %s", path)
			}
			item.Exists, item.Mode = true, info.Mode().Perm()
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				item.UID, item.GID = int(stat.Uid), int(stat.Gid)
			} else {
				return nil, fmt.Errorf("cannot read file ownership: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dir, item.Backup), data, 0600); err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		tx.files = append(tx.files, item)
	}
	manifest, err := json.MarshalIndent(tx.files, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := system.AtomicWrite(filepath.Join(dir, "manifest.json"), manifest, 0600, -1, -1); err != nil {
		return nil, err
	}
	return tx, nil
}

// restore 独立尝试每个文件，不能因某个文件失败而跳过其余恢复步骤。
func (tx *transaction) restore() error {
	var failures []error
	for _, item := range tx.files {
		if !item.Exists {
			if err := system.SafePath(item.Path); err != nil {
				failures = append(failures, err)
				continue
			}
			if err := os.Remove(item.Path); err != nil && !os.IsNotExist(err) {
				failures = append(failures, err)
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(tx.dir, item.Backup))
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := system.AtomicWrite(item.Path, data, item.Mode, item.UID, item.GID); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%v", failures)
	}
	return nil
}
