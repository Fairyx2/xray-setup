package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"my-vpn/internal/config"
	"my-vpn/internal/message"
	"my-vpn/internal/system"
)

// Plan 保存待部署内容和打开配置时的基线，用于发现界面操作期间发生的外部修改。
type Plan struct {
	Profile config.Profile  // 客户端连接和主入站参数。
	Native  json.RawMessage // 完整原生 Xray JSON，保留高级扩展。
	Base    json.RawMessage // 修改前的实际配置；首次安装时为空。
}

func (s *Service) Apply(ctx context.Context, plan Plan, emit func(message.Message)) (*config.State, error) {
	if err := s.CheckHost(ctx); err != nil {
		return nil, err
	}
	unlock, err := acquireLock(s.Paths.Lock)
	if err != nil {
		return nil, message.Fail("error.lock", err.Error())
	}
	defer unlock()
	profile, err := config.ParseNative(plan.Native, plan.Profile)
	if err != nil {
		return nil, message.Fail("error.config", system.Clean(err.Error()))
	}
	previous, err := s.Load()
	if err != nil {
		return nil, message.Fail("error.state", err.Error())
	}
	if previous != nil && !bytes.Equal(previous.Config, plan.Base) || previous == nil && len(plan.Base) != 0 {
		return nil, message.Fail("error.changed", "")
	}
	if previous == nil {
		loaded, err := s.run(ctx, "systemctl", "show", "--property=LoadState", "--value", UnitName)
		if err != nil || strings.TrimSpace(loaded) != "not-found" {
			return nil, message.Fail("error.existing", UnitName)
		}
		for _, path := range []string{s.Paths.Config, s.Paths.Binary, s.Paths.Unit, "/usr/local/bin/xray", "/usr/local/etc/xray/config.json", "/etc/xray/config.json"} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return nil, message.Fail("error.existing", path)
			}
		}
	}
	emit(message.New("progress.target"))
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = ProbeTarget(probeCtx, profile.Target, profile.ServerName)
	cancel()
	if err != nil {
		return nil, s.commandError("error.target", profile, "", err)
	}
	if ctx.Err() != nil {
		return nil, message.Fail("error.cancelled", "")
	}
	version, stagedBinary := "", ""
	if previous == nil {
		emit(message.New("progress.release"))
		release, err := s.Latest(ctx)
		if err != nil {
			return nil, s.commandError("error.download", profile, "", err)
		}
		version = release.Tag
		// 下载、SHA-256 校验和原生配置预检均先完成，再停止/修改实际服务。
		stage, err := os.MkdirTemp("", "my-vpn-stage-*")
		if err != nil {
			return nil, message.Fail("error.files", err.Error())
		}
		defer os.RemoveAll(stage)
		emit(message.New("progress.download", release.Tag))
		stagedBinary, err = s.StageRelease(ctx, release, stage)
		if err != nil {
			return nil, s.commandError("error.download", profile, "", err)
		}
	} else {
		version, stagedBinary = previous.Version, s.Paths.Binary
	}
	stagedConfig, err := os.CreateTemp("", "my-vpn-config-*.json")
	if err != nil {
		return nil, message.Fail("error.files", err.Error())
	}
	defer os.Remove(stagedConfig.Name())
	if _, err := stagedConfig.Write(plan.Native); err != nil {
		stagedConfig.Close()
		return nil, message.Fail("error.files", err.Error())
	}
	if err := stagedConfig.Close(); err != nil {
		return nil, message.Fail("error.files", err.Error())
	}
	emit(message.New("progress.validate"))
	output, err := s.run(ctx, stagedBinary, "run", "-test", "-config", stagedConfig.Name())
	if err != nil {
		return nil, s.commandError("error.xray_test", profile, output, err)
	}
	if ctx.Err() != nil {
		return nil, message.Fail("error.cancelled", "")
	}
	gid, err := s.ensureAccount(ctx)
	if err != nil {
		return nil, message.Fail("error.account", system.Clean(err.Error()))
	}
	if err := s.prepareDirectories(gid); err != nil {
		return nil, message.Fail("error.files", err.Error())
	}
	paths := []string{s.Paths.Config, s.Paths.Unit, s.Paths.State}
	if previous == nil {
		paths = append(paths, s.Paths.Binary)
	}
	tx, err := newTransaction(s.Paths.Backups, paths)
	if err != nil {
		return nil, message.Fail("error.backup", err.Error())
	}
	wasActive, wasEnabled := false, false
	if previous != nil {
		// 捕获恢复基线时不沿用已取消的上下文，防止把查询失败误当成原服务未运行。
		stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		wasActive, wasEnabled, err = s.recoveryState(stateCtx)
		cancel()
		if err != nil {
			return nil, message.Fail("error.service", err.Error())
		}
	}
	if ctx.Err() != nil {
		return nil, message.Fail("error.cancelled", "")
	}
	// 将恢复前的服务状态一起落盘，供断电/强制终止后的人工恢复参考。
	recovery, _ := json.MarshalIndent(struct {
		Unit    string `json:"unit"`        // 被本工具管理的单元名。
		Active  bool   `json:"was_active"`  // 部署前是否运行。
		Enabled bool   `json:"was_enabled"` // 部署前是否启用开机启动。
	}{UnitName, wasActive, wasEnabled}, "", "  ")
	if err := system.AtomicWrite(filepath.Join(tx.dir, "service-state.json"), recovery, 0600, 0, 0); err != nil {
		return nil, message.Fail("error.backup", err.Error())
	}
	// 自此进入事务：任何错误（包括取消）都通过独立超时上下文恢复原文件和原服务状态。
	committed := false
	defer func() {
		if committed && previous == nil {
			_ = os.RemoveAll(tx.dir)
		}
	}()
	rollback := func(cause error) (*config.State, error) {
		emit(message.New("progress.rollback"))
		restoreCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var failures []string
		_, unitErr := os.Stat(s.Paths.Unit)
		if previous != nil || unitErr == nil {
			if _, err := s.run(restoreCtx, "systemctl", "stop", UnitName); err != nil {
				failures = append(failures, err.Error())
			}
			if !wasEnabled {
				if _, err := s.run(restoreCtx, "systemctl", "disable", UnitName); err != nil {
					failures = append(failures, err.Error())
				}
			}
		}
		if err := tx.restore(); err != nil {
			failures = append(failures, err.Error())
		}
		if _, err := s.run(restoreCtx, "systemctl", "daemon-reload"); err != nil {
			failures = append(failures, err.Error())
		}
		if wasEnabled {
			if _, err := s.run(restoreCtx, "systemctl", "enable", UnitName); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if wasActive && previous != nil {
			if _, err := s.run(restoreCtx, "systemctl", "start", UnitName); err != nil {
				failures = append(failures, err.Error())
			} else if err := s.waitHealthy(restoreCtx, previous.Profile); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return nil, message.Fail("error.rollback", profile.Redact(system.Clean(strings.Join(failures, "\n"))), tx.dir)
		}
		emit(message.New("notice.restored", tx.dir))
		return nil, message.Wrap("error.restored", tx.dir, cause)
	}
	emit(message.New("progress.backup", tx.dir))
	if wasActive {
		if output, err := s.run(ctx, "systemctl", "stop", UnitName); err != nil {
			return rollback(s.commandError("error.service", profile, output, err))
		}
	}
	if !PortAvailable(profile.Listen, profile.Port) {
		return rollback(message.Fail("error.port_busy", "", profile.Port))
	}
	emit(message.New("progress.write"))
	if previous == nil {
		binary, err := os.ReadFile(stagedBinary)
		if err != nil {
			return rollback(message.Fail("error.files", err.Error()))
		}
		if err := system.AtomicWrite(s.Paths.Binary, binary, 0755, 0, 0); err != nil {
			return rollback(message.Fail("error.files", err.Error()))
		}
	}
	if err := system.AtomicWrite(s.Paths.Config, plan.Native, 0640, 0, gid); err != nil {
		return rollback(message.Fail("error.files", err.Error()))
	}
	unit := s.unitFile()
	if err := system.AtomicWrite(s.Paths.Unit, []byte(unit), 0644, 0, 0); err != nil {
		return rollback(message.Fail("error.files", err.Error()))
	}
	// 再以服务账号检查实际文件，发现权限、外部规则文件可读性等 root 预检无法发现的问题。
	output, err = s.run(ctx, "runuser", "-u", ServiceUser, "--", s.Paths.Binary, "run", "-test", "-config", s.Paths.Config)
	if err != nil {
		return rollback(s.commandError("error.xray_test", profile, output, err))
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", UnitName}, {"restart", UnitName}} {
		emit(message.New("progress.service"))
		if output, err := s.run(ctx, "systemctl", args...); err != nil {
			return rollback(s.commandError("error.service", profile, output, err))
		}
	}
	emit(message.New("progress.health"))
	if err := s.waitHealthy(ctx, profile); err != nil {
		logs, _ := s.run(ctx, "journalctl", "-u", UnitName, "-n", "15", "--no-pager", "--output=cat")
		return rollback(s.commandError("error.health", profile, logs, err))
	}
	state := &config.State{Schema: 1, Version: version, Profile: profile, Config: plan.Native}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return rollback(message.Fail("error.files", err.Error()))
	}
	if ctx.Err() != nil {
		return rollback(message.Fail("error.cancelled", ""))
	}
	if err := system.AtomicWrite(s.Paths.State, data, 0600, 0, 0); err != nil {
		return rollback(message.Fail("error.files", err.Error()))
	}
	committed = true
	return state, nil
}

func (s *Service) ensureAccount(ctx context.Context) (int, error) {
	account, err := user.Lookup(ServiceUser)
	if err != nil {
		if _, missing := err.(user.UnknownUserError); !missing {
			return 0, err
		}
		output, err := s.run(ctx, "useradd", "--system", "--user-group", "--no-create-home", "--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", ServiceUser)
		if err != nil {
			return 0, fmt.Errorf("%w: %s", err, output)
		}
		account, err = user.Lookup(ServiceUser)
		if err != nil {
			return 0, err
		}
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return 0, err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return 0, err
	}
	if uid == 0 || gid == 0 || account.HomeDir != "/nonexistent" || uid >= 1000 {
		return 0, fmt.Errorf("existing account %s is not a dedicated system account", ServiceUser)
	}
	group, err := user.LookupGroup(ServiceUser)
	if err != nil || group.Gid != account.Gid {
		return 0, fmt.Errorf("service account must use the dedicated %s group", ServiceUser)
	}
	return gid, nil
}

func (s *Service) unitFile() string {
	return fmt.Sprintf(`%s
[Unit]
Description=my-vpn Xray (VLESS + XHTTP + REALITY)
After=network-online.target nss-lookup.target
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
ExecStart=%s run -config %s
Restart=on-failure
RestartSec=3
RestartPreventExitStatus=23
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
UMask=0077
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, managedMarker, ServiceUser, ServiceUser, filepath.Clean(s.Paths.Binary), filepath.Clean(s.Paths.Config))
}
