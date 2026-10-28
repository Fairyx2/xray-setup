// Package system 封装受限命令执行和文件写入；不把用户输入拼接到 shell 命令。
package system

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}

type ExecRunner struct{}

// limitedBuffer 避免异常服务日志无限占用内存，超出部分直接丢弃。
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if left := 64*1024 - b.Len(); left > 0 {
		if left < len(p) {
			p = p[:left]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var output limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	text := Clean(output.String())
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return text, fmt.Errorf("%s: %w", name, err)
	}
	return text, nil
}

var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))`)

// Clean 移除外部诊断中的 ANSI/控制字符，防止服务日志改变终端状态。
func Clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansiEscape.ReplaceAllString(text, ""))
}
