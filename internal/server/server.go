// Package server 提供与 TUI 无关的检测、安装和服务管理用例。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"my-vpn/internal/config"
	"my-vpn/internal/message"
	"my-vpn/internal/system"
)

const UnitName = "my-vpn-xray.service"
const ServiceUser = "my-vpn"
const managedMarker = "# Managed by my-vpn"

// Paths 集中声明服务器上由本工具管理的路径，测试使用临时目录替换，不操作宿主机服务。
type Paths struct {
	Binary  string // 官方 Xray 可执行文件；与其他工具常见的 /usr/local/bin/xray 分开存放。
	Config  string // 当前生效的原生 Xray JSON，root:my-vpn，0640。
	Unit    string // systemd 单元文件，包含明确的工具管理标记。
	State   string // 表单参数和完整配置，root 专用，0600。
	Backups string // 配置快照和失败恢复材料，目录权限为 0700。
	Lock    string // 防止两个本工具实例同时修改服务的运行锁。
}

func DefaultPaths() Paths {
	return Paths{
		Binary: "/usr/local/lib/my-vpn/xray", Config: "/etc/my-vpn/xray.json",
		Unit: "/etc/systemd/system/" + UnitName, State: "/var/lib/my-vpn/state.json",
		Backups: "/var/lib/my-vpn/backups", Lock: "/run/my-vpn.lock",
	}
}

type Service struct {
	Paths      Paths
	Runner     system.Runner
	HTTP       *http.Client
	GOOS, Arch string
	EUID       func() int
}

func New() *Service {
	return &Service{Paths: DefaultPaths(), Runner: system.ExecRunner{},
		HTTP: &http.Client{Timeout: 2 * time.Minute}, GOOS: runtime.GOOS, Arch: runtime.GOARCH, EUID: os.Geteuid}
}

// Report 是只读预检的结果；没有公网 IPv4 或候选目标时允许用户进入表单补充。
type Report struct {
	Profile  config.Profile
	Existing *config.State
	Notices  []message.Message
}

func (s *Service) Load() (*config.State, error) {
	for _, path := range []string{s.Paths.State, s.Paths.Config, s.Paths.Unit, s.Paths.Binary} {
		if err := system.SafePath(path); err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(s.Paths.State)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state config.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Schema != 1 {
		return nil, fmt.Errorf("unsupported state schema")
	}
	unit, err := os.ReadFile(s.Paths.Unit)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(unit), managedMarker+"\n") {
		return nil, fmt.Errorf("service ownership mismatch")
	}
	// 原生文件可能在终端中被手动修改，以磁盘上的实际配置为准，避免静默覆盖。
	raw, err := os.ReadFile(s.Paths.Config)
	if err != nil {
		return nil, err
	}
	state.Profile, err = config.ParseNative(raw, state.Profile)
	if err != nil {
		return nil, err
	}
	state.Config = raw
	return &state, nil
}

func (s *Service) CheckHost(ctx context.Context) error {
	if s.GOOS != "linux" {
		return message.Fail("error.platform", "")
	}
	if s.EUID() != 0 {
		return message.Fail("error.root", "")
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return message.Fail("error.platform", err.Error())
	}
	isDebian := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") && strings.Trim(strings.TrimPrefix(line, "ID="), `"`) == "debian" {
			isDebian = true
		}
	}
	if !isDebian {
		return message.Fail("error.platform", "")
	}
	if _, err := AssetName(s.Arch); err != nil {
		return message.Fail("error.arch", s.Arch)
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return message.Fail("error.systemd", "")
	}
	return nil
}

func (s *Service) Inspect(ctx context.Context, emit func(message.Message)) (Report, error) {
	emit(message.New("progress.environment"))
	if err := s.CheckHost(ctx); err != nil {
		return Report{}, err
	}
	state, err := s.Load()
	if err != nil {
		return Report{}, message.Fail("error.state", system.Clean(err.Error()))
	}
	if state != nil {
		return Report{Profile: state.Profile, Existing: state}, nil
	}
	for _, path := range []string{s.Paths.Config, s.Paths.Binary, s.Paths.Unit, "/usr/local/etc/xray/config.json", "/etc/xray/config.json", "/usr/local/bin/xray"} {
		if _, err := os.Lstat(path); err == nil {
			return Report{}, message.Fail("error.existing", path)
		} else if !os.IsNotExist(err) {
			return Report{}, message.Fail("error.inspect", err.Error())
		}
	}
	profile, err := config.Generate()
	if err != nil {
		return Report{}, message.Fail("error.credentials", err.Error())
	}
	report := Report{Profile: profile}
	emit(message.New("progress.port"))
	port, occupied, err := s.RecommendPort(ctx)
	if err != nil {
		return report, message.Fail("error.port", err.Error())
	}
	report.Profile.Port = port
	if occupied != "" {
		report.Notices = append(report.Notices, message.New("notice.port", occupied, port))
	}
	emit(message.New("progress.address"))
	address, err := s.DiscoverAddress(ctx)
	if err == nil {
		report.Profile.Address = address
	} else {
		report.Notices = append(report.Notices, message.New("notice.address"))
	}
	emit(message.New("progress.target"))
	targets := []string{"www.microsoft.com", "www.apple.com", "www.amazon.com"}
	found := false
	for _, host := range targets {
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		err := ProbeTarget(probeCtx, host+":443", host)
		cancel()
		if err == nil {
			report.Profile.Target, report.Profile.ServerName = host+":443", host
			found = true
			break
		}
	}
	if !found {
		report.Notices = append(report.Notices, message.New("notice.target"))
	}
	return report, nil
}

func (s *Service) run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return s.Runner.Run(ctx, name, args...)
}

func (s *Service) commandError(key string, p config.Profile, output string, err error) error {
	detail := p.Redact(system.Clean(strings.TrimSpace(fmt.Sprint(err) + "\n" + output)))
	if data, readErr := os.ReadFile(s.Paths.Config); readErr == nil {
		detail = config.RedactNative(data, detail)
	}
	return message.Fail(key, detail)
}

func ensureDirectory(path string, mode os.FileMode, gid int) error {
	if err := system.SafePath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if gid >= 0 {
		return os.Chown(path, 0, gid)
	}
	return nil
}

func (s *Service) prepareDirectories(gid int) error {
	for _, item := range []struct {
		path string
		mode os.FileMode
		gid  int
	}{
		{filepath.Dir(s.Paths.Binary), 0755, 0}, {filepath.Dir(s.Paths.Config), 0750, gid},
		{filepath.Dir(s.Paths.State), 0700, 0}, {s.Paths.Backups, 0700, 0},
	} {
		if err := ensureDirectory(item.path, item.mode, item.gid); err != nil {
			return err
		}
	}
	return nil
}
