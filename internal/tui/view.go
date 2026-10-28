package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"my-vpn/internal/config"
	"my-vpn/internal/system"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("120"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func (m Model) View() string {
	if m.width < 40 || m.height < 14 {
		return "my-vpn\n" + m.tr.T("terminal.small") + "\nCtrl+C"
	}
	width := min(m.width-4, 100)
	header := titleStyle.Render("my-vpn") + "  " + mutedStyle.Render("VLESS + XHTTP + REALITY")
	if m.demo {
		header += "\n" + warningStyle.Render(m.tr.T("demo.banner"))
	}
	var body string
	switch m.screen {
	case languageScreen:
		body = "选择语言 / Choose language"
	case welcomeScreen:
		body = m.tr.T("welcome.title") + "\n\n" + m.tr.T("welcome.description")
	case reviewScreen:
		mode := m.tr.T("mode.simple")
		if m.advanced {
			mode = m.tr.T("mode.advanced")
		}
		body = titleStyle.Render(m.tr.T("review.title")) + " · " + mode + "\n\n" + m.summary()
		for _, notice := range m.notices {
			body += "\n" + warningStyle.Render(m.tr.Message(notice))
		}
		body += "\n\n" + mutedStyle.Render(m.tr.T("review.credentials")) + "\n" + mutedStyle.Render(m.tr.T("review.scope"))
	case workingScreen:
		spinner := []string{"◐", "◓", "◑", "◒"}[m.frame%4]
		body = titleStyle.Render(spinner+" "+m.tr.T("working.title")) + "\n\n"
		start := max(0, len(m.progress)-max(3, m.height-13))
		for i, event := range m.progress[start:] {
			prefix := "✓ "
			if i == len(m.progress[start:])-1 {
				prefix = "› "
			}
			body += prefix + m.tr.Message(event) + "\n"
		}
		if m.cancelling {
			body += "\n" + warningStyle.Render(m.tr.T("working.cancelling"))
		}
	case formScreen:
		body = m.formView(width)
	case menuScreen:
		p := m.installed.Profile
		state := m.tr.T("service.inactive")
		if m.status.Active {
			state = m.tr.T("service.active")
		}
		enabled := m.tr.T("no")
		if m.status.Enabled {
			enabled = m.tr.T("yes")
		}
		body = titleStyle.Render(m.tr.T("menu.title")) + "\n\n" + fmt.Sprintf("%s · %s\n%s %s\nTCP %d · Xray %s\n", state, p.Address, m.tr.T("service.enabled"), enabled, p.Port, m.installed.Version)
		body += "\n" + warningStyle.Render(m.tr.T("firewall.note", p.Port)) + "\n" + mutedStyle.Render(m.tr.T("health.note"))
	case outputScreen, shareScreen:
		body = m.outputView(width)
	case stopScreen:
		body = warningStyle.Render(m.tr.T("stop.confirm"))
	}
	if m.info.Key != "" && (m.screen == reviewScreen || m.screen == menuScreen) {
		body += "\n\n" + selectedStyle.Render(m.tr.Message(m.info))
	}
	if m.err != nil {
		diagnostic := config.RedactNative(m.native, m.profile.Redact(system.Clean(m.tr.Error(m.err))))
		if m.installed != nil {
			diagnostic = config.RedactNative(m.installed.Config, diagnostic)
		}
		text := ansi.Hardwrap(diagnostic, width, true)
		lines := strings.Split(text, "\n")
		if len(lines) > 8 {
			lines = append(lines[:8], m.tr.T("error.truncated"))
		}
		body = errorStyle.Render(strings.Join(lines, "\n")) + "\n\n" + body
	}
	help := m.tr.T("help.menu")
	switch m.screen {
	case languageScreen:
		help = "↑/↓ · Enter · Ctrl+C"
	case formScreen:
		help = m.tr.T("help.form")
	case workingScreen:
		help = m.tr.T("help.working")
	case outputScreen:
		help = m.tr.T("help.output")
	case shareScreen:
		help = m.tr.T("help.share")
	}
	// 逐行限制宽度，防止长分享链接和日志破坏 SSH 小窗口布局。
	body = ansi.Hardwrap(body, width, true)
	header = ansi.Hardwrap(header, width, true)
	available := max(1, m.height-len(strings.Split(header, "\n"))-7)
	menu := ""
	if options := m.options(); len(options) > 0 {
		// 菜单固定在内容视口下方；小窗口仍能看到当前操作，长摘要通过 PgUp/PgDn 阅读。
		menu = m.menuView(max(1, min(len(options), available/2)), width)
		available = max(1, available-len(strings.Split(menu, "\n"))-1)
	}
	lines := strings.Split(body, "\n")
	if len(lines) > available {
		start := min(m.scroll, len(lines)-available)
		if m.screen == formScreen {
			for i, line := range lines {
				if strings.HasPrefix(ansi.Strip(line), "›") && i+1 >= start+available {
					start = max(0, i-1)
					break
				}
			}
			start = min(start, len(lines)-available)
		}
		body = strings.Join(lines[start:min(len(lines), start+available)], "\n")
		help = m.tr.T("help.scroll") + " · " + help
	}
	if menu != "" {
		body += "\n\n" + menu
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(header + "\n\n" + body + "\n\n" + mutedStyle.Render(ansi.Truncate(help, width, "…")))
}

func (m Model) menuView(maxLines, width int) string {
	var out strings.Builder
	options := m.options()
	start := max(0, m.cursor-maxLines+1)
	for i := start; i < min(len(options), start+maxLines); i++ {
		option := options[i]
		line := fmt.Sprintf("  %d  %s", i+1, option.label)
		if i == m.cursor {
			line = selectedStyle.Render("›" + line[1:])
		}
		out.WriteString(ansi.Truncate(line, width, "…") + "\n")
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func (m Model) formView(width int) string {
	var out strings.Builder
	out.WriteString(titleStyle.Render(m.tr.T("form.title")) + "\n")
	if m.height >= 22 {
		out.WriteString(mutedStyle.Render(m.tr.T("form.defaults")) + "\n")
	}
	out.WriteString("\n")
	visible := max(1, (m.height-15)/3)
	start := max(0, m.focus-visible+1)
	end := min(len(m.fields), start+visible)
	for i := start; i < end; i++ {
		f := m.fields[i]
		label := fmt.Sprintf("%d/%d  %s", i+1, len(m.fields), m.tr.T("field."+f.key))
		value := f.value
		if f.secret {
			value = strings.Repeat("•", len([]rune(value)))
		}
		if i == m.focus {
			runes := []rune(value)
			at := min(m.caret, len(runes))
			value = string(runes[:at]) + "▏" + string(runes[at:])
			out.WriteString(selectedStyle.Render("› "+label) + "\n")
		} else {
			out.WriteString("  " + label + "\n")
		}
		out.WriteString("  " + ansi.Truncate(value, width-2, "…") + "\n\n")
	}
	out.WriteString(m.tr.T("hint." + m.fields[m.focus].key))
	return out.String()
}

func (m Model) outputText() string {
	if m.screen != shareScreen {
		return m.output
	}
	p := m.installed.Profile
	return m.tr.T("share.private") + "\n\n" + p.ShareLinkFor(m.installed.Config) + "\n\n" +
		fmt.Sprintf("%s: %s\n%s: %d\nUUID: %s\nSNI: %s\nPublic key / Password: %s\nShort ID: %s\nPath: %s\nMode: %s\nFingerprint: %s\nFlow: %s\nMux: %s",
			m.tr.T("field.address"), p.Address, m.tr.T("field.port"), p.Port, p.UUID, p.ServerName, p.PublicKey, p.ShortID, p.Path, p.Mode, p.Fingerprint, m.tr.T("empty"), m.tr.T("disabled"))
}

func (m Model) maxScroll() int {
	width := max(10, min(m.width-4, 100))
	header := "my-vpn  VLESS + XHTTP + REALITY"
	if m.demo {
		header += "\n" + m.tr.T("demo.banner")
	}
	available := max(1, m.height-len(strings.Split(ansi.Hardwrap(header, width, true), "\n"))-7)
	return max(0, len(strings.Split(ansi.Hardwrap(m.outputView(width), width, true), "\n"))-available)
}

func (m Model) outputView(width int) string {
	title := m.outputTitle
	if m.screen == shareScreen {
		title = m.tr.T("menu.share")
	}
	text := m.outputText()
	if text == "" {
		text = m.tr.T("logs.empty")
	}
	return titleStyle.Render(title) + "\n\n" + text
}

func (m Model) summary() string {
	p := m.profile
	rows := [][2]string{{"field.address", p.Address}, {"field.port", fmt.Sprint(p.Port)}, {"field.target", p.Target}, {"field.server_name", p.ServerName}}
	if m.advanced {
		rows = append(rows, [2]string{"field.listen", p.Listen}, [2]string{"field.path", p.Path}, [2]string{"field.mode", p.Mode}, [2]string{"field.log_level", p.LogLevel})
	}
	var out strings.Builder
	for _, row := range rows {
		value := row[1]
		if value == "" {
			value = m.tr.T("missing")
		}
		fmt.Fprintf(&out, "%s  %s\n", m.tr.T(row[0]), value)
	}
	return out.String()
}
