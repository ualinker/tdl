package logutil

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func NewStdout(level zapcore.LevelEnabler) *zap.Logger {
	stdoutWriter := &noopSyncWriter{os.Stdout}

	config := zap.NewDevelopmentEncoderConfig()
	config.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	config.EncodeLevel = zapcore.CapitalLevelEncoder

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(config), stdoutWriter, level)
	return zap.New(core, zap.AddCaller())
}

func New(level zapcore.LevelEnabler, path string) *zap.Logger {
	rotate := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    10,
		MaxAge:     7,
		MaxBackups: 3,
		LocalTime:  true,
		Compress:   true,
	}

	writer := zapcore.AddSync(rotate)

	config := zap.NewDevelopmentEncoderConfig()
	config.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	config.EncodeLevel = zapcore.CapitalLevelEncoder

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(config), writer, level)
	return zap.New(core, zap.AddCaller())
}

type noopSyncWriter struct {
	zapcore.WriteSyncer
}

func (w *noopSyncWriter) Sync() error {
	// A no-op Sync() call to prevent the "inappropriate ioctl for device" panic
	return nil
}
