// Package tui 实现页面状态机；所有安装和网络操作都通过异步消息执行，避免阻塞键盘响应。
package tui

import (
	"context"
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"my-vpn/internal/config"
	"my-vpn/internal/i18n"
	"my-vpn/internal/message"
	"my-vpn/internal/server"
)

type screen int

const (
	languageScreen screen = iota
	welcomeScreen
	workingScreen
	reviewScreen
	formScreen
	menuScreen
	outputScreen
	shareScreen
	stopScreen
)

// Model 只保存界面状态及待确认配置；Profile/Native 在点击部署前不会写入系统配置目录。
type Model struct {
	service                              *server.Service
	ctx                                  context.Context
	tr                                   i18n.Translator
	demo                                 bool
	screen, returnScreen                 screen
	advanced                             bool
	cursor, width, height, scroll, frame int
	profile                              config.Profile
	native, base, editorDraft            json.RawMessage
	installed                            *config.State
	status                               server.Status
	notices                              []message.Message
	progress                             []message.Message
	err                                  error
	info                                 message.Message
	output, outputTitle                  string
	fields                               []field
	focus, caret                         int
	events                               chan tea.Msg
	cancel                               context.CancelFunc
	cancelling                           bool
	printLink                            bool
	jobID                                uint64
}

type inspectDone struct {
	report server.Report
	err    error
}
type applyDone struct {
	state *config.State
	err   error
}
type statusDone struct {
	status  server.Status
	details bool
}
type logsDone struct {
	output string
	err    error
}
type controlDone struct {
	err    error
	action string
}
type tickMsg struct{ jobID uint64 }
type CancelMsg struct{}

func New(ctx context.Context, service *server.Service, language i18n.Language, demo bool) Model {
	cursor := 0
	if language == i18n.English {
		cursor = 1
	}
	return Model{ctx: ctx, service: service, tr: i18n.New(language), demo: demo, screen: languageScreen, cursor: cursor, width: 88, height: 30}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Export() string {
	if m.printLink && m.installed != nil {
		return m.installed.Profile.ShareLinkFor(m.installed.Config)
	}
	return ""
}

func (m *Model) startJob(back screen, fn func(context.Context, func(message.Message)) tea.Msg) tea.Cmd {
	m.screen, m.returnScreen = workingScreen, back
	m.progress, m.scroll = nil, 0
	m.jobID++
	m.cancelling = false
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	ch := make(chan tea.Msg, 64)
	m.events = ch
	return tea.Batch(func() tea.Msg {
		go func() {
			emit := func(event message.Message) { ch <- event }
			ch <- fn(ctx, emit)
		}()
		return <-ch
	}, tick(m.jobID))
}

func tick(jobID uint64) tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{jobID: jobID} })
}
func waitEvent(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

func (m *Model) finishJob() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.cancelling = false
	m.cursor, m.scroll = 0, 0
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.frame++
		if m.screen == workingScreen && msg.jobID == m.jobID {
			return m, tick(m.jobID)
		}
		return m, nil
	case CancelMsg:
		if m.screen == workingScreen {
			m.cancelling = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		return m, tea.Quit
	case message.Message:
		m.progress = append(m.progress, msg)
		return m, waitEvent(m.events)
	case inspectDone:
		m.finishJob()
		if msg.err != nil {
			m.screen, m.err = welcomeScreen, msg.err
			return m, nil
		}
		m.profile, m.installed, m.notices = msg.report.Profile, msg.report.Existing, msg.report.Notices
		if m.installed != nil {
			m.native = append([]byte(nil), m.installed.Config...)
			m.base = append([]byte(nil), m.installed.Config...)
			m.screen = menuScreen
			return m, m.loadStatus()
		}
		m.native, _ = config.Build(m.profile)
		m.screen = reviewScreen
		return m, nil
	case applyDone:
		m.finishJob()
		if msg.err != nil {
			m.screen, m.err = reviewScreen, msg.err
			return m, nil
		}
		m.installed, m.profile, m.native = msg.state, msg.state.Profile, msg.state.Config
		if m.demo {
			m.status = server.Status{Active: true, Enabled: true}
		}
		m.base = append([]byte(nil), msg.state.Config...)
		m.screen, m.info = menuScreen, message.New("deploy.success")
		return m, m.loadStatus()
	case statusDone:
		m.finishJob()
		m.status = msg.status
		m.screen = menuScreen
		if msg.details {
			m.screen, m.returnScreen = outputScreen, menuScreen
			m.output, m.outputTitle = msg.status.Detail, m.tr.T("menu.status")
		}
		return m, nil
	case logsDone:
		m.finishJob()
		m.screen = outputScreen
		m.outputTitle, m.output, m.err = m.tr.T("menu.logs"), msg.output, msg.err
		return m, nil
	case controlDone:
		m.finishJob()
		m.screen, m.err = menuScreen, msg.err
		if msg.err == nil {
			m.info = message.New("service.done")
			if m.demo {
				m.status.Active = msg.action != "stop"
			}
		}
		return m, m.loadStatus()
	case editorDone:
		m.screen = reviewScreen
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.editorDraft = msg.data
		p, err := config.ParseNative(msg.data, m.profile)
		if err != nil {
			m.err = message.Fail("error.native", m.profile.Redact(err.Error()))
			return m, nil
		}
		m.profile, m.native, m.editorDraft, m.err = p, msg.data, nil, nil
		m.info = message.New("native.updated")
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			if m.screen == workingScreen {
				m.cancelling = true
				if m.cancel != nil {
					m.cancel()
				}
				return m, nil
			}
			return m, tea.Quit
		}
		if m.screen == workingScreen {
			return m, nil
		}
		if m.screen == formScreen {
			return m.updateForm(msg)
		}
		if m.screen == outputScreen || m.screen == shareScreen {
			switch key {
			case "esc", "q", "enter":
				m.screen, m.scroll = m.returnScreen, 0
			case "up", "k":
				if m.scroll > 0 {
					m.scroll--
				}
			case "down", "j":
				m.scroll = min(m.scroll+1, m.maxScroll())
			case "pgdown":
				m.scroll = min(m.scroll+max(1, m.height-10), m.maxScroll())
			case "pgup":
				m.scroll = max(0, m.scroll-max(1, m.height-10))
			case "p":
				if m.screen == shareScreen {
					m.printLink = true
					return m, tea.Quit
				}
			}
			return m, nil
		}
		if key == "q" {
			return m, tea.Quit
		}
		if key == "esc" {
			switch m.screen {
			case languageScreen:
				return m, tea.Quit
			case welcomeScreen:
				m.screen, m.cursor = languageScreen, 0
			case reviewScreen:
				if m.installed != nil {
					m.profile = m.installed.Profile
					m.native = append([]byte(nil), m.installed.Config...)
					m.screen = menuScreen
				} else {
					m.screen = welcomeScreen
				}
				m.cursor, m.err = 0, nil
				m.info, m.editorDraft = message.Message{}, nil
			case stopScreen:
				m.screen, m.cursor = menuScreen, 0
			}
			return m, nil
		}
		options := m.options()
		switch key {
		case "pgdown":
			m.scroll += max(1, m.height/2)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height/2))
		case "up", "k":
			m.scroll = 0
			m.cursor = (m.cursor + len(options) - 1) % len(options)
		case "down", "j", "tab":
			m.scroll = 0
			m.cursor = (m.cursor + 1) % len(options)
		case "enter":
			return m.choose(options[m.cursor].id)
		default:
			if len(key) == 1 && key[0] >= '1' && int(key[0]-'1') < len(options) {
				m.cursor = int(key[0] - '1')
				return m.choose(options[m.cursor].id)
			}
		}
	}
	return m, nil
}
