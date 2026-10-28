package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"my-vpn/internal/message"
	"my-vpn/internal/system"
)

type editorDone struct {
	data json.RawMessage
	err  error
}

func (m *Model) editNative() tea.Cmd {
	dir, err := os.MkdirTemp("", "my-vpn-editor-*")
	if err != nil {
		return func() tea.Msg { return editorDone{err: message.Fail("error.editor", err.Error())} }
	}
	// macOS 的 /var 和 /tmp 是系统符号链接；仅规范化工具自己刚创建的私有临时目录。
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		os.RemoveAll(dir)
		return func() tea.Msg { return editorDone{err: message.Fail("error.editor", err.Error())} }
	}
	dir = resolved
	path := filepath.Join(dir, "xray.json")
	data := m.native
	if m.editorDraft != nil {
		data = m.editorDraft
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		os.RemoveAll(dir)
		return func() tea.Msg { return editorDone{err: message.Fail("error.editor", err.Error())} }
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		for _, candidate := range []string{"nano", "vi"} {
			if _, err := exec.LookPath(candidate); err == nil {
				editor = candidate
				break
			}
		}
	}
	// 使用 argv 执行编辑器，不通过 shell 展开，环境变量中的 ;、$() 等不会被解释。
	args := strings.Fields(editor)
	if len(args) == 0 {
		os.RemoveAll(dir)
		return func() tea.Msg { return editorDone{err: message.Fail("error.editor_missing", "")} }
	}
	cmd := exec.Command(args[0], append(args[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.RemoveAll(dir)
		if err != nil {
			return editorDone{err: message.Fail("error.editor", system.Clean(err.Error()))}
		}
		if err := system.SafePath(path); err != nil {
			return editorDone{err: message.Fail("error.editor", err.Error())}
		}
		info, err := os.Stat(path)
		if err != nil {
			return editorDone{err: message.Fail("error.editor", err.Error())}
		}
		if !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return editorDone{err: message.Fail("error.editor", fmt.Sprint("file exceeds 2 MiB or is not regular"))}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return editorDone{err: message.Fail("error.editor", err.Error())}
		}
		return editorDone{data: data}
	})
}
