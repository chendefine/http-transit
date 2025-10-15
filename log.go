package main

import (
	"io"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	log *zap.SugaredLogger
)

func init() {
	SetLogger("info", "")
}

func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format(time.DateTime))
}

func customLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString("[" + level.CapitalString() + "]")
}

func SetLogger(level string, file string) (string, string) {
	level = strings.ToLower(level)
	zlevel := zap.InfoLevel
	switch level {
	case "debug":
		zlevel = zap.DebugLevel
	case "info":
		zlevel = zap.InfoLevel
	case "warn", "warning":
		zlevel = zap.WarnLevel
	case "error":
		zlevel = zap.ErrorLevel
	case "dpanic":
		zlevel = zap.DPanicLevel
	case "panic":
		zlevel = zap.PanicLevel
	case "fatal":
		zlevel = zap.FatalLevel
	default:
		level = "info"
	}

	var zfile io.Writer = os.Stderr
	if file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			file = ""
			log.Errorf("打开日志文件失败: %v", err)
		} else {
			zfile = io.MultiWriter(os.Stderr, f)
		}
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:          "time",
		LevelKey:         "level",
		MessageKey:       "message",
		EncodeTime:       customTimeEncoder,
		EncodeLevel:      customLevelEncoder,
		EncodeDuration:   zapcore.StringDurationEncoder,
		ConsoleSeparator: " ",
	}

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), zapcore.AddSync(zfile), zlevel)
	log = zap.New(core).Sugar()
	return level, file
}
