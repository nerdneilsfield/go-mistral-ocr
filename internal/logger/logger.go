package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// InitLogger 初始化日志记录器
func InitLogger(level, format, logFile string) (*zap.Logger, error) {
	// 解析日志级别
	var zapLevel zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	// 创建基本配置
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	// 设置输出格式
	switch strings.ToLower(format) {
	case "json":
		config.Encoding = "json"
	default:
		config.Encoding = "console"
	}

	// 配置输出
	if logFile == "" {
		// 输出到控制台
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}
	} else {
		// 确保日志目录存在
		logDir := filepath.Dir(logFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, fmt.Errorf("无法创建日志目录: %w", err)
		}
		config.OutputPaths = []string{logFile}
		config.ErrorOutputPaths = []string{logFile}
	}

	// 配置日志字段
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.NameKey = "logger"
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// 创建日志记录器
	logger, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		return nil, fmt.Errorf("创建日志记录器失败: %w", err)
	}

	// 使用敏感信息打码包装器
	logger = NewMaskedLogger(logger)

	return logger, nil
}

// GetLevel 获取日志级别
func GetLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// LevelNames 返回所有可用的日志级别名称
func LevelNames() []string {
	return []string{"debug", "info", "warn", "error"}
}
