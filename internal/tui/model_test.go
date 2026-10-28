package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"my-vpn/internal/config"
	"my-vpn/internal/i18n"
	"my-vpn/internal/message"
	"my-vpn/internal/server"
)

func fixture(t *testing.T, language i18n.Language) Model {
	t.Helper()
	m := New(context.Background(), server.New(), language, true)
	p, err := config.Generate()
	if err != nil {
		t.Fatal(err)
	}
	p.Address = "203.0.113.10"
	m.profile = p
	m.native, _ = config.Build(p)
	m.installed = &config.State{Schema: 1, Profile: p, Config: m.native, Version: "demo"}
	return m
}

func TestLanguageIsChosenBeforeAnySystemWork(t *testing.T) {
	m := New(context.Background(), nil, i18n.Chinese, false)
	if m.screen != languageScreen || m.Init() != nil || m.cursor != 0 {
		t.Fatal("wrong initial state")
	}
	if !strings.Contains(m.View(), "English") || !strings.Contains(m.View(), "简体中文") {
		t.Fatal("missing language choices")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.tr.Language != i18n.English || m.screen != welcomeScreen {
		t.Fatal("language selection ran system work or failed")
	}
}

func TestSimpleFormRetainsDefaultsAndSynchronizesTarget(t *testing.T) {
	m := fixture(t, i18n.Chinese)
	original := m.profile
	m.setupForm()
	if len(m.fields) != 3 {
		t.Fatal("simple mode asks for too many fields")
	}
	for i := range m.fields {
		if m.fields[i].key == "port" {
			m.fields[i].value = ""
		}
		if m.fields[i].key == "target" {
			m.fields[i].value = "www.apple.com"
		}
	}
	if err := m.saveForm(); err != nil {
		t.Fatal(err)
	}
	if m.profile.Port != original.Port || m.profile.UUID != original.UUID || m.profile.PrivateKey != original.PrivateKey {
		t.Fatal("changed defaults or credentials")
	}
	if m.profile.Target != "www.apple.com:443" || m.profile.ServerName != "www.apple.com" {
		t.Fatal("simple mode did not synchronize SNI")
	}
}

func TestAdvancedFormMasksPrivateKeyAndRejectsInvalidPort(t *testing.T) {
	m := fixture(t, i18n.English)
	m.advanced = true
	m.setupForm()
	m.screen = formScreen
	if len(m.fields) < 10 {
		t.Fatal("advanced fields missing")
	}
	for i, f := range m.fields {
		if f.key == "private_key" {
			m.focus = i
		}
	}
	if strings.Contains(m.View(), m.profile.PrivateKey) {
		t.Fatal("private key visible")
	}
	original := m.profile
	for i := range m.fields {
		if m.fields[i].key == "port" {
			m.fields[i].value = "65536"
		}
	}
	if m.saveForm() == nil || m.profile != original {
		t.Fatal("invalid form changed pending profile")
	}
}

func TestInvalidNativeDraftCanBeCorrectedWithoutChangingLivePlan(t *testing.T) {
	m := fixture(t, i18n.Chinese)
	original := string(m.native)
	next, _ := m.Update(editorDone{data: []byte(`{"bad":`)})
	m = next.(Model)
	if m.err == nil || m.editorDraft == nil || string(m.native) != original {
		t.Fatal("invalid draft not retained separately")
	}
	next, _ = m.Update(editorDone{data: []byte(original)})
	m = next.(Model)
	if m.err != nil || m.editorDraft != nil {
		t.Fatal("corrected draft not accepted")
	}
}

func TestCancelWaitsForJobAndPreservesFailureAcrossStatusRefresh(t *testing.T) {
	m := fixture(t, i18n.Chinese)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.screen = workingScreen
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)
	if cmd != nil || !m.cancelling || ctx.Err() == nil || m.screen != workingScreen {
		t.Fatal("quit before job recovery")
	}
	next, _ = m.Update(controlDone{err: message.Fail("error.service", "failed")})
	m = next.(Model)
	next, _ = m.Update(statusDone{status: server.Status{Active: false}})
	m = next.(Model)
	if m.err == nil {
		t.Fatal("status refresh erased failure")
	}
}

func TestStaleSpinnerDoesNotCreateAnotherTimer(t *testing.T) {
	m := fixture(t, i18n.Chinese)
	m.screen = workingScreen
	m.jobID = 2
	_, cmd := m.Update(tickMsg{jobID: 1})
	if cmd != nil {
		t.Fatal("stale timer restarted")
	}
}

func TestSummaryKeepsActionsVisibleAndShareCanScrollToEnd(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		m := fixture(t, language)
		m.width, m.height = 80, 24
		m.screen = reviewScreen
		view := ansi.Strip(m.View())
		for _, key := range []string{"review.deploy", "review.edit", "review.toggle", "back"} {
			if !strings.Contains(view, m.tr.T(key)) {
				t.Fatalf("hidden menu action %s in %s", key, language)
			}
		}
		m.screen = shareScreen
		m.scroll = m.maxScroll()
		view = ansi.Strip(m.View())
		if !strings.Contains(view, "Mux:") || !strings.Contains(view, m.tr.T("disabled")) {
			t.Fatal("cannot scroll to final connection parameter")
		}
	}
}

func TestDemoStopUpdatesStatus(t *testing.T) {
	m := fixture(t, i18n.Chinese)
	m.screen = workingScreen
	m.status = server.Status{Active: true, Enabled: true}
	next, _ := m.Update(controlDone{action: "stop"})
	m = next.(Model)
	if m.status.Active {
		t.Fatal("demo stop retained active status")
	}
}

func TestEveryScreenFitsSmallTerminalsAndBothLanguages(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, size := range [][2]int{{40, 14}, {80, 24}, {100, 40}} {
			for _, page := range []screen{languageScreen, welcomeScreen, reviewScreen, formScreen, menuScreen, outputScreen, shareScreen, stopScreen, workingScreen} {
				m := fixture(t, language)
				m.screen = page
				m.width, m.height = size[0], size[1]
				m.advanced = true
				m.setupForm()
				m.output = strings.Repeat("long log line with text\n", 100)
				view := ansi.Strip(m.View())
				if len(strings.Split(view, "\n")) > m.height {
					t.Fatalf("%s page %d exceeds height %d", language, page, m.height)
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("%s page %d exceeds width %d: %s", language, page, m.width, line)
					}
				}
				if strings.Contains(view, "[hint.") || strings.Contains(view, "[field.") {
					t.Fatal("missing translation")
				}
			}
		}
	}
}
