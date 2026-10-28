// Package i18n 使用内嵌 JSON 词典，运行时不依赖外部语言文件。
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	"my-vpn/internal/message"
)

//go:embed locales/*.json
var resources embed.FS

type Language string

const (
	Chinese Language = "zh"
	English Language = "en"
)

type Translator struct {
	Language Language
	words    map[string]string
}

func New(language Language) Translator {
	if language != English {
		language = Chinese
	}
	data, err := resources.ReadFile("locales/" + string(language) + ".json")
	if err != nil {
		panic(err)
	}
	t := Translator{Language: language}
	if err := json.Unmarshal(data, &t.words); err != nil {
		panic(err)
	}
	return t
}

func (t Translator) T(key string, args ...any) string {
	value, ok := t.words[key]
	if !ok {
		return "[" + key + "]"
	}
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}

func (t Translator) Message(m message.Message) string { return t.T(m.Key, m.Args...) }

func (t Translator) Error(err error) string {
	var localized *message.Error
	if errors.As(err, &localized) {
		text := t.Message(localized.Message)
		if localized.Cause != nil {
			text += "\n" + t.Error(localized.Cause)
		}
		if localized.Detail != "" {
			text += "\n" + localized.Detail
		}
		return text
	}
	return t.T("error.unexpected") + "\n" + err.Error()
}
