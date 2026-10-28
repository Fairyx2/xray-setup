package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"my-vpn/internal/i18n"
	"my-vpn/internal/server"
	"my-vpn/internal/tui"
)

var version = "0.1.0-dev"

func main() {
	demo := flag.Bool("demo", false, "本地预览，不修改服务器 / Preview without server changes")
	language := flag.String("lang", "zh", "初始选中的语言 zh/en（启动时仍可选择） / Initial language selection")
	showVersion := flag.Bool("version", false, "显示版本 / Print version")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), "my-vpn — VLESS + XHTTP + REALITY\n\n用法 / Usage: my-vpn [--demo] [--lang zh|en]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *language != "zh" && *language != "en" {
		fmt.Fprintln(os.Stderr, "语言必须为 zh 或 en / Language must be zh or en")
		os.Exit(2)
	}
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := tui.New(ctx, server.New(), i18n.Language(*language), *demo)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithoutSignalHandler())
	// 将 SIGTERM/SIGINT 转为界面取消事件；部署过程中先回滚，避免框架立即退出中断恢复。
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			program.Send(tui.CancelMsg{})
		case <-ctx.Done():
		}
	}()
	result, err := program.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "TUI:", err)
		os.Exit(1)
	}
	if final, ok := result.(tui.Model); ok {
		if link := final.Export(); link != "" {
			fmt.Println(link)
		}
	}
}
