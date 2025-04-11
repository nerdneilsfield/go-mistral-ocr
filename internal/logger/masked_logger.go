package logger

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 敏感信息类型
const (
	APIKey   = "api_key"
	Password = "password"
	Token    = "token"
)

// MaskSensitiveInfo 对敏感信息进行打码
func MaskSensitiveInfo(info string, infoType string) string {
	if info == "" {
		return ""
	}

	switch infoType {
	case APIKey, Password, Token:
		if len(info) <= 8 {
			return "****"
		}
		// 保留前4位和后4位，中间用*替代
		return info[:4] + strings.Repeat("*", len(info)-8) + info[len(info)-4:]
	default:
		return info
	}
}

// NewMaskedLogger 创建一个会对敏感信息进行打码的日志记录器
func NewMaskedLogger(baseLogger *zap.Logger) *zap.Logger {
	return baseLogger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return &maskedCore{Core: core}
	}))
}

// maskedCore 是一个自定义的 zapcore.Core，用于对敏感信息进行打码
type maskedCore struct {
	zapcore.Core
}

// Check 实现 zapcore.Core 接口
func (c *maskedCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// Write 实现 zapcore.Core 接口，对敏感字段进行打码
func (c *maskedCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// 对敏感字段进行打码
	for i, field := range fields {
		if isSensitiveField(field.Key) {
			// 对字符串类型的敏感字段进行打码
			if field.Type == zapcore.StringType {
				fields[i] = zap.String(field.Key, MaskSensitiveInfo(field.String, getFieldType(field.Key)))
			}
		}
	}
	return c.Core.Write(entry, fields)
}

// isSensitiveField 判断字段是否为敏感字段
func isSensitiveField(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "auth")
}

// getFieldType 根据字段名获取敏感信息类型
func getFieldType(key string) string {
	key = strings.ToLower(key)
	if strings.Contains(key, "api_key") || strings.Contains(key, "apikey") {
		return APIKey
	}
	if strings.Contains(key, "password") {
		return Password
	}
	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "auth") {
		return Token
	}
	return ""
}
