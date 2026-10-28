package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"my-vpn/internal/config"
	"my-vpn/internal/message"
	"my-vpn/internal/server"
	"time"
)

func (m *Model) inspect() tea.Cmd {
	demo, service := m.demo, m.service
	return m.startJob(welcomeScreen, func(ctx context.Context, emit func(message.Message)) tea.Msg {
		if demo {
			emit(message.New("progress.environment"))
			p, err := config.Generate()
			p.Address, p.Port = "203.0.113.10", 8443
			return inspectDone{report: server.Report{Profile: p, Notices: []message.Message{message.New("notice.port", "nginx (demo)", 8443)}}, err: err}
		}
		report, err := service.Inspect(ctx, emit)
		return inspectDone{report: report, err: err}
	})
}

func (m *Model) deploy() tea.Cmd {
	plan := server.Plan{Profile: m.profile, Native: append([]byte(nil), m.native...), Base: append([]byte(nil), m.base...)}
	demo, service := m.demo, m.service
	return m.startJob(reviewScreen, func(ctx context.Context, emit func(message.Message)) tea.Msg {
		if demo {
			for _, key := range []string{"progress.validate", "progress.write", "progress.service", "progress.health"} {
				emit(message.New(key))
				select {
				case <-ctx.Done():
					return applyDone{err: message.Fail("error.cancelled", "")}
				case <-time.After(150 * time.Millisecond):
				}
			}
			return applyDone{state: &config.State{Schema: 1, Version: "demo", Profile: plan.Profile, Config: plan.Native}}
		}
		state, err := service.Apply(ctx, plan, emit)
		return applyDone{state: state, err: err}
	})
}

func (m *Model) loadStatus(details ...bool) tea.Cmd {
	demo, service, profile := m.demo, m.service, m.installed.Profile
	demoStatus := m.status
	showDetails := len(details) > 0 && details[0]
	return m.startJob(menuScreen, func(ctx context.Context, emit func(message.Message)) tea.Msg {
		emit(message.New("progress.status"))
		if demo {
			demoStatus.Detail = "my-vpn-xray.service\n" + fmt.Sprint("Active: ", demoStatus.Active, "\nEnabled: ", demoStatus.Enabled)
			return statusDone{status: demoStatus, details: showDetails}
		}
		return statusDone{status: service.Status(ctx, profile), details: showDetails}
	})
}

func (m *Model) loadLogs() tea.Cmd {
	demo, service, profile := m.demo, m.service, m.installed.Profile
	return m.startJob(menuScreen, func(ctx context.Context, emit func(message.Message)) tea.Msg {
		if demo {
			return logsDone{output: "[demo] Xray service started\n[demo] VLESS + XHTTP + REALITY listening on TCP 8443"}
		}
		output, err := service.Logs(ctx, profile)
		return logsDone{output: output, err: err}
	})
}

func (m *Model) control(action string) tea.Cmd {
	demo, service := m.demo, m.service
	return m.startJob(menuScreen, func(ctx context.Context, emit func(message.Message)) tea.Msg {
		emit(message.New("progress.service"))
		if demo {
			return controlDone{action: action}
		}
		return controlDone{err: service.Control(ctx, action)}
	})
}
