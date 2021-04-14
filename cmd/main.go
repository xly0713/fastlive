package main

import (
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	rtmpserver "fastlive/pkg/rtmp/server"
)

func main() {
	logger, _ := zap.NewProduction()
	srv, err := rtmpserver.New()
	if err != nil {
		logger.Error("create rtmp server instance", zap.Error(err))
		return
	}

	if err := srv.ListenAndServe(); err != nil {
		logger.Error("rtmp server listen and serve", zap.Error(err))
		return
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
}
