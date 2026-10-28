package i18n

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"my-vpn/internal/message"
)

func TestLocaleKeysAndPlaceholdersMatch(t *testing.T) {
	zh, en := New(Chinese), New(English)
	if len(zh.words) != len(en.words) {
		t.Fatal("locale key count differs")
	}
	placeholder := regexp.MustCompile(`%[sdv]`)
	for key, value := range zh.words {
		other, ok := en.words[key]
		if !ok || other == "" {
			t.Errorf("missing English key: %s", key)
			continue
		}
		if fmt.Sprint(placeholder.FindAllString(value, -1)) != fmt.Sprint(placeholder.FindAllString(other, -1)) {
			t.Errorf("placeholder mismatch: %s", key)
		}
	}
	// 确保资源内容是有效 JSON，避免新增翻译时误用注释或尾随逗号。
	for _, lang := range []string{"zh", "en"} {
		data, _ := resources.ReadFile("locales/" + lang + ".json")
		if !json.Valid(data) {
			t.Fatal("invalid JSON", lang)
		}
	}
}

func TestDefaultLanguageAndNestedRecoveryError(t *testing.T) {
	tr := New("")
	if tr.Language != Chinese {
		t.Fatal("default language is not Chinese")
	}
	err := message.Wrap("error.restored", "/backup", message.Fail("error.port_busy", "", 8443))
	for _, lang := range []Language{Chinese, English} {
		text := New(lang).Error(err)
		if !strings.Contains(text, "8443") || !strings.Contains(text, "/backup") || strings.Contains(text, "error.port_busy") {
			t.Fatalf("untranslated error: %s", text)
		}
	}
}
