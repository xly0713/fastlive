package server

import (
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

type config struct {
	Addr           string // 监听地址，默认 ":1935"
	ReadBufSize    int    // server创建的连接读数据缓冲区大小(默认8192字节)
	LocalChunkSize uint32 //for chunk Multiplexing(默认60000字节)

	// 超时控制参数
	HandshakeTimeout     time.Duration
	PlayorPublishTimeout time.Duration

	// 日志配置
	Log log

	// pprof debug开关
	EnablePprof bool
}

type log struct {
	Path         string
	Level        string
	RotationTime time.Duration
	Age          int
}

func (s *Server) loadConfig(configPath string) error {
	viper.SetConfigFile("config.yaml")
	viper.SetConfigName("config")
	viper.AddConfigPath(configPath)

	if err := viper.ReadInConfig(); err != nil {
		return errors.Wrap(err, "read in config")
	}

	if s.config == nil {
		s.config = new(config)
	}

	if err := viper.Unmarshal(s.config); err != nil {
		return errors.Wrap(err, "Unmarshal config")
	}

	return nil
}

func getAbsConfigPath() (string, error) {
	binPath, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(filepath.Dir(binPath), "config")
	return configPath, nil
}
