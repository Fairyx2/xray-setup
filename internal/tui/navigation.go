package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"my-vpn/internal/config"
	"my-vpn/internal/i18n"
	"my-vpn/internal/message"
)

type option struct{ id, label string }

func (m Model) options() []option {
	switch m.screen {
	case languageScreen:
		return []option{{"zh", "简体中文（默认）"}, {"en", "English"}}
	case welcomeScreen:
		return []option{{"simple", m.tr.T("mode.simple")}, {"advanced", m.tr.T("mode.advanced")}, {"language", m.tr.T("language.change")}, {"quit", m.tr.T("quit")}}
	case reviewScreen:
		options := []option{{"deploy", m.tr.T("review.deploy")}, {"edit", m.tr.T("review.edit")}, {"toggle", m.tr.T("review.toggle")}}
		if m.advanced {
			options = append(options, option{"native", m.tr.T("review.native")}, option{"preview", m.tr.T("review.preview")})
		}
		return append(options, option{"back", m.tr.T("back")})
	case menuScreen:
		return []option{{"status", m.tr.T("menu.status")}, {"logs", m.tr.T("menu.logs")}, {"editConfig", m.tr.T("menu.edit")}, {"share", m.tr.T("menu.share")}, {"start", m.tr.T("menu.start")}, {"restart", m.tr.T("menu.restart")}, {"stopConfirm", m.tr.T("menu.stop")}, {"language", m.tr.T("language.change")}, {"quit", m.tr.T("quit")}}
	case stopScreen:
		return []option{{"back", m.tr.T("back")}, {"stop", m.tr.T("menu.stop")}}
	}
	return nil
}

func (m Model) choose(id string) (tea.Model, tea.Cmd) {
	m.err = nil
	m.scroll = 0
	switch id {
	case "zh", "en":
		m.tr, m.cursor = i18n.New(i18n.Language(id)), 0
		if m.installed != nil {
			m.screen = menuScreen
		} else {
			m.screen = welcomeScreen
		}
	case "language":
		m.screen, m.cursor = languageScreen, 0
		if m.tr.Language == i18n.English {
			m.cursor = 1
		}
	case "simple", "advanced":
		m.advanced = id == "advanced"
		return m, m.inspect()
	case "toggle":
		m.advanced, m.cursor = !m.advanced, 0
	case "edit":
		m.setupForm()
		m.screen = formScreen
	case "native":
		return m, m.editNative()
	case "preview":
		m.outputTitle, m.output, m.returnScreen, m.screen, m.scroll = m.tr.T("review.preview"), config.RedactNative(m.native, m.profile.Redact(string(m.native))), reviewScreen, outputScreen, 0
	case "deploy":
		if err := m.profile.Validate(); err != nil {
			m.err = message.Fail("error.field", "", m.tr.T("field."+err.Error()))
			return m, nil
		}
		return m, m.deploy()
	case "editConfig":
		m.profile = m.installed.Profile
		m.native = append([]byte(nil), m.installed.Config...)
		m.base = append([]byte(nil), m.installed.Config...)
		m.screen, m.cursor, m.notices, m.info = reviewScreen, 0, nil, message.Message{}
	case "status":
		return m, m.loadStatus(true)
	case "logs":
		return m, m.loadLogs()
	case "share":
		m.screen, m.returnScreen, m.scroll = shareScreen, menuScreen, 0
	case "start", "restart", "stop":
		return m, m.control(id)
	case "stopConfirm":
		m.screen, m.cursor = stopScreen, 0
	case "back":
		m.cursor = 0
		if m.installed != nil {
			m.profile = m.installed.Profile
			m.native = append([]byte(nil), m.installed.Config...)
			m.screen = menuScreen
		} else {
			m.screen = welcomeScreen
		}
		m.info, m.editorDraft = message.Message{}, nil
	case "quit":
		return m, tea.Quit
	}
	return m, nil
}
