package server

import (
	"net"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"fastlive/pkg/rtmp/chunk"
)

type Server struct {
	configPath string
	config     *config
	logger     *zap.Logger

	broker *broker //流会话管理器

	decodeHdrPool *sync.Pool //读取rtmp chunk头部时使用的[]byte池
	encodeHdrPool *sync.Pool //rtmp chunk header编码使用的[]byte池 (at most 18 bytes)
	newChunkPool  *sync.Pool //创建chunk块时使用
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return errors.Wrap(err, "net listen")
	}

	if s.config.EnablePprof {
		go func() {
			if err := http.ListenAndServe("localhost:6060", nil); err != nil {
				s.logger.Error("listen http pprof", zap.Error(err))
			}
		}()
	}

	return s.Serve(ln)
}

func (s *Server) Serve(l net.Listener) error {
	for {
		rwc, err := l.Accept()
		if err != nil {
			return errors.Wrap(err, "listener accept")
		}

		serverConn, err := newServerConn(
			WithServerConnServer(s),
			WithServerConnRawConn(rwc),
			WithServerConnReadBufSize(s.config.ReadBufSize),
			WithServerConnLocalChunkSize(s.config.LocalChunkSize),
			WithServerConnReadHdrPoll(s.decodeHdrPool),
			WithServerConnChunkEncodePool(s.encodeHdrPool),
			WithServerConnNewChunkPool(s.newChunkPool),
		)
		if err != nil {
			return errors.Wrap(err, "create server conn")
		}

		go serverConn.serve()
	}
}

func New(opts ...serverOption) (*Server, error) {
	s, err := (&Server{}).loadOptions(opts...)
	if err != nil {
		return nil, errors.Wrap(err, "load options")
	}

	return s, nil
}

func (s *Server) loadOptions(opts ...serverOption) (*Server, error) {
	for _, opt := range opts {
		opt(s)
	}

	if s.configPath == "" {
		var err error
		if s.configPath, err = getAbsConfigPath(); err != nil {
			return nil, errors.Wrap(err, "get abs config path while config path not assigned")
		}
	}
	if err := s.loadConfig(s.configPath); err != nil {
		return nil, errors.Wrap(err, "load config")
	}

	if err := s.initLogger(); err != nil {
		return nil, errors.Wrap(err, "init logger")
	}

	if s.config.Addr == "" {
		s.config.Addr = ":1935"
	}

	if s.config.ReadBufSize <= 0 {
		s.config.ReadBufSize = 8192
	}

	if s.config.LocalChunkSize <= 0 {
		s.config.LocalChunkSize = 60000 //bytes
	}

	if s.config.HandshakeTimeout <= 0 {
		s.config.HandshakeTimeout = 3 * time.Second
	}

	if s.config.PlayorPublishTimeout <= 0 {
		s.config.PlayorPublishTimeout = 3 * time.Second
	}

	if s.broker == nil {
		if b, err := newBroker(
			WithBrokerServer(s),
		); err != nil {
			return nil, errors.Wrap(err, "create session broker")
		} else {
			s.broker = b
		}
	}

	if s.decodeHdrPool == nil {
		s.decodeHdrPool = &sync.Pool{
			New: func() interface{} {
				//basic header: at most 3 bytes, message Header: at most 11 bytes(Sequentially read)
				return make([]byte, 11)
			},
		}
	}

	if s.encodeHdrPool == nil {
		s.encodeHdrPool = &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 18)
				return buf
			},
		}
	}

	if s.newChunkPool == nil {
		s.newChunkPool = &sync.Pool{
			New: func() interface{} {
				ck := chunk.New()
				return ck
			},
		}
	}

	return s, nil
}

type serverOption func(*Server)

func WithConfigPath(p string) serverOption {
	return func(s *Server) {
		s.configPath = p
	}
}
