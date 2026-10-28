package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"my-vpn/internal/config"
	"my-vpn/internal/message"
)

type field struct {
	key, value, original string
	secret               bool
}

func (m *Model) setupForm() {
	p := m.profile
	m.fields = []field{
		{key: "address", value: p.Address}, {key: "port", value: strconv.Itoa(p.Port)}, {key: "target", value: p.Target},
	}
	if m.advanced {
		m.fields = append(m.fields,
			field{key: "server_name", value: p.ServerName}, field{key: "listen", value: p.Listen},
			field{key: "uuid", value: p.UUID}, field{key: "private_key", value: p.PrivateKey, secret: true},
			field{key: "short_id", value: p.ShortID}, field{key: "path", value: p.Path}, field{key: "mode", value: p.Mode},
			field{key: "fingerprint", value: p.Fingerprint}, field{key: "log_level", value: p.LogLevel})
	}
	for i := range m.fields {
		m.fields[i].original = m.fields[i].value
	}
	m.focus, m.caret, m.err = 0, len([]rune(m.fields[0].value)), nil
}

func (m Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.fields[m.focus]
	switch msg.String() {
	case "pgdown":
		m.scroll += max(1, m.height/2)
		return m, nil
	case "pgup":
		m.scroll = max(0, m.scroll-max(1, m.height/2))
		return m, nil
	case "esc":
		m.screen, m.err = reviewScreen, nil
		return m, nil
	case "ctrl+s":
		if err := m.saveForm(); err != nil {
			m.err = err
			return m, nil
		}
		m.screen, m.err, m.info = reviewScreen, nil, message.New("form.saved")
		return m, nil
	case "tab", "down", "enter":
		m.scroll = 0
		m.focus = (m.focus + 1) % len(m.fields)
		m.caret = len([]rune(m.fields[m.focus].value))
		return m, nil
	case "shift+tab", "up":
		m.scroll = 0
		m.focus = (m.focus + len(m.fields) - 1) % len(m.fields)
		m.caret = len([]rune(m.fields[m.focus].value))
		return m, nil
	case "ctrl+u":
		f.value, m.caret = "", 0
		return m, nil
	}
	runes := []rune(f.value)
	switch msg.String() {
	case "left":
		m.caret = max(0, m.caret-1)
	case "right":
		m.caret = min(len(runes), m.caret+1)
	case "home", "ctrl+a":
		m.caret = 0
	case "end", "ctrl+e":
		m.caret = len(runes)
	case "backspace", "ctrl+h":
		if m.caret > 0 {
			runes = append(runes[:m.caret-1], runes[m.caret:]...)
			m.caret--
			f.value = string(runes)
		}
	case "delete":
		if m.caret < len(runes) {
			runes = append(runes[:m.caret], runes[m.caret+1:]...)
			f.value = string(runes)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			input := msg.Runes
			if msg.Type == tea.KeySpace {
				input = []rune{' '}
			}
			var clean []rune
			for _, r := range input {
				if !unicode.IsControl(r) {
					clean = append(clean, r)
				}
			}
			if len(runes)+len(clean) <= 512 {
				updated := append([]rune(nil), runes[:m.caret]...)
				updated = append(updated, clean...)
				updated = append(updated, runes[m.caret:]...)
				f.value = string(updated)
				m.caret += len(clean)
			}
		}
	}
	return m, nil
}

func (m *Model) saveForm() error {
	p := m.profile
	for _, field := range m.fields {
		value := strings.TrimSpace(field.value)
		if value == "" {
			value = field.original
		}
		switch field.key {
		case "address":
			p.Address = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return message.Fail("error.field", "", m.tr.T("field.port"))
			}
			p.Port = port
		case "target":
			if !strings.Contains(value, ":") {
				value += ":443"
			}
			p.Target = value
			if !m.advanced {
				if host, _, err := net.SplitHostPort(value); err == nil {
					p.ServerName = host
				}
			}
		case "server_name":
			p.ServerName = value
		case "listen":
			p.Listen = value
		case "uuid":
			p.UUID = value
		case "private_key":
			p.PrivateKey = value
		case "short_id":
			p.ShortID = value
		case "path":
			p.Path = value
		case "mode":
			p.Mode = value
		case "fingerprint":
			p.Fingerprint = value
		case "log_level":
			p.LogLevel = value
		}
	}
	public, err := config.PublicFromPrivate(p.PrivateKey)
	if err != nil {
		return message.Fail("error.field", "", m.tr.T("field.private_key"))
	}
	p.PublicKey = public
	if err := p.Validate(); err != nil {
		return message.Fail("error.field", "", m.tr.T("field."+err.Error()))
	}
	native, err := config.Patch(m.native, p)
	if err != nil {
		return message.Fail("error.config", fmt.Sprint(err))
	}
	if _, err := config.ParseNative(native, p); err != nil {
		return message.Fail("error.native", p.Redact(err.Error()))
	}
	m.profile, m.native = p, native
	return nil
}
