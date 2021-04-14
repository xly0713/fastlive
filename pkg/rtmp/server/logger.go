package server

import (
	"os"
	"path"
	"path/filepath"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func (s *Server) initLogger() error {
	log := s.config.Log
	logPath, err := getAbsLogPath(log.Path)
	if err != nil {
		return errors.Wrap(err, "get abs log path")
	}

	age := log.Age
	if age <= 0 {
		age = 7
	}
	maxAge := time.Duration(age) * 24 * time.Hour

	rotationTime := log.RotationTime
	if rotationTime <= 0 {
		rotationTime = 24 * time.Hour
	}

	rotator, err := rotatelogs.New(
		logPath+"_%Y%m%d",
		rotatelogs.WithLinkName(logPath),
		rotatelogs.WithMaxAge(maxAge),
		rotatelogs.WithRotationTime(rotationTime),
	)
	if err != nil {
		return errors.Wrap(err, "create log rotator")
	}

	level := zap.NewAtomicLevel()
	if err := level.UnmarshalText([]byte(log.Level)); err != nil {
		return errors.Wrap(err, "invalid log level")
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	w := zapcore.AddSync(rotator)
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		w,
		level.Level(),
	)

	s.logger = zap.New(core, zap.AddCaller())

	return nil
}

func getAbsLogPath(p string) (string, error) {
	if path.IsAbs(p) {
		return p, nil
	}

	binPath, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "", err
	}

	logPath := filepath.Join(filepath.Dir(binPath), p)
	return logPath, nil
}
