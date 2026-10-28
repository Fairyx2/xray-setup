package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"my-vpn/internal/config"
	"my-vpn/internal/message"
	"my-vpn/internal/system"
)

type Status struct {
	Active  bool   // systemd 单元是否处于 active 状态。
	Enabled bool   // 是否启用开机启动。
	Detail  string // systemctl 状态信息，展示前清理控制字符并脱敏。
}

func (s *Service) recoveryState(ctx context.Context) (bool, bool, error) {
	output, err := s.run(ctx, "systemctl", "show", "--property=ActiveState", "--property=UnitFileState", UnitName)
	if err != nil {
		return false, false, err
	}
	var active, unit string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ActiveState=") {
			active = strings.TrimPrefix(line, "ActiveState=")
		}
		if strings.HasPrefix(line, "UnitFileState=") {
			unit = strings.TrimPrefix(line, "UnitFileState=")
		}
	}
	if active == "" || unit == "" {
		return false, false, fmt.Errorf("incomplete systemd recovery state")
	}
	if unit != "enabled" && unit != "disabled" {
		return false, false, fmt.Errorf("unsupported unit state: %s", unit)
	}
	return active == "active" || active == "activating" || active == "reloading", unit == "enabled", nil
}

func (s *Service) active(ctx context.Context) bool {
	text, err := s.run(ctx, "systemctl", "is-active", UnitName)
	return err == nil && strings.TrimSpace(text) == "active"
}

func (s *Service) enabled(ctx context.Context) bool {
	text, err := s.run(ctx, "systemctl", "is-enabled", UnitName)
	return err == nil && strings.TrimSpace(text) == "enabled"
}

func (s *Service) Status(ctx context.Context, profile config.Profile) Status {
	output, _ := s.run(ctx, "systemctl", "status", UnitName, "--no-pager", "--full")
	if data, err := os.ReadFile(s.Paths.Config); err == nil {
		output = config.RedactNative(data, output)
	}
	return Status{Active: s.active(ctx), Enabled: s.enabled(ctx), Detail: profile.Redact(system.Clean(output))}
}

func (s *Service) Logs(ctx context.Context, profile config.Profile) (string, error) {
	output, err := s.run(ctx, "journalctl", "-u", UnitName, "-n", "80", "--no-pager", "--output=short-iso")
	if err != nil {
		return "", s.commandError("error.logs", profile, output, err)
	}
	output = profile.Redact(system.Clean(output))
	if data, err := os.ReadFile(s.Paths.Config); err == nil {
		output = config.RedactNative(data, output)
	}
	return output, nil
}

func (s *Service) Control(ctx context.Context, action string) error {
	if err := s.CheckHost(ctx); err != nil {
		return err
	}
	if action != "start" && action != "stop" && action != "restart" {
		return message.Fail("error.service", "invalid action")
	}
	unlock, err := acquireLock(s.Paths.Lock)
	if err != nil {
		return message.Fail("error.lock", err.Error())
	}
	defer unlock()
	state, err := s.Load()
	if err != nil || state == nil {
		return message.Fail("error.state", fmt.Sprint(err))
	}
	if action != "stop" {
		output, err := s.run(ctx, "runuser", "-u", ServiceUser, "--", s.Paths.Binary, "run", "-test", "-config", s.Paths.Config)
		if err != nil {
			return s.commandError("error.xray_test", state.Profile, output, err)
		}
	}
	output, err := s.run(ctx, "systemctl", action, UnitName)
	if err != nil {
		return s.commandError("error.service", state.Profile, output, err)
	}
	if action != "stop" {
		if err := s.waitHealthy(ctx, state.Profile); err != nil {
			return s.commandError("error.health", state.Profile, "", err)
		}
	}
	return nil
}

func (s *Service) waitHealthy(ctx context.Context, profile config.Profile) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	listen := profile.Listen
	if listen == "0.0.0.0" {
		listen = "127.0.0.1"
	}
	// 连续两次检查服务及 TCP，避免把进程刚启动后立即退出误判成成功。
	successes := 0
	for {
		if s.active(ctx) {
			conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(listen, fmt.Sprint(profile.Port)))
			if err == nil {
				conn.Close()
				successes++
			} else {
				successes = 0
			}
		} else {
			successes = 0
		}
		if successes >= 2 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
