// Package message 定义不依赖界面语言的消息，让系统操作与翻译资源保持解耦。
package message

import "fmt"

// Message 在业务层只携带翻译键及参数，到界面层才转换成中文或英文。
type Message struct {
	Key  string
	Args []any
}

func New(key string, args ...any) Message { return Message{Key: key, Args: args} }

// Error 的 Detail 用于保留操作系统诊断；调用方必须先移除凭据和终端控制字符。
type Error struct {
	Message Message
	Detail  string
	Cause   error
}

func (e *Error) Unwrap() error { return e.Cause }

func Wrap(key, detail string, cause error, args ...any) error {
	return &Error{Message: New(key, args...), Detail: detail, Cause: cause}
}

func (e *Error) Error() string { return fmt.Sprint(e.Message.Key, ": ", e.Detail) }

func Fail(key, detail string, args ...any) error {
	return &Error{Message: New(key, args...), Detail: detail}
}
