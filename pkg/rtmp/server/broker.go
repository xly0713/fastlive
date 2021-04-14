package server

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"fastlive/pkg/av/flv"
)

type broker struct {
	server *Server

	sessionTotal int32    // 发布的流session数目计数
	sessionMap   sync.Map // key: vhost+app+stream value: session

	demuxer *flv.Demuxer // flv解码器(仅关注header)
}

func (b *broker) createSession(publisher *conn, vhost, streamKey, sessionId string) (*session, error) {
	appName := publisher.clientConnectInfo.app
	streamName := publisher.clientPublishOrPlayInfo.stream

	if value, ok := b.sessionMap.Load(streamKey); ok {
		sess := value.(*session)
		if sess.publisher == nil { // session短暂中断, publisher软删除被重置为nil
			sess.publisher = publisher
			if sess.id != sessionId {
				//TODO: warn
				sess.id = sessionId
			}
			return sess, nil
		}

		return nil, errors.Errorf("session exists, vhost: %s, app: %s, stream: %s", vhost, appName, streamName)
	}

	sess, err := newSession(
		WithSessionId(sessionId),
		WithSessionVhost(vhost),
		WithSessionAppName(appName),
		WithSessionStreamName(streamName),
		WithSessionPublisher(publisher),
		WithSessionBroker(b),
		WithSessionStreamKey(streamKey),
	)
	if err != nil {
		return nil, errors.Wrap(err, "new session instance")
	} else {
		b.sessionMap.Store(streamKey, sess)
		atomic.AddInt32(&b.sessionTotal, 1)
	}

	return sess, nil
}

func (b *broker) softDelSession(streamKey string) error {
	value, ok := b.sessionMap.Load(streamKey)
	if !ok {
		return errors.Errorf("session not exists, streamKey: %s", streamKey)
	}

	sess := value.(*session)
	sess.publisher = nil
	sess.offline <- true

	return nil
}

func (b *broker) delSession(streamKey string) {
	b.sessionMap.Delete(streamKey)
	atomic.AddInt32(&b.sessionTotal, -1)
}

func (b *broker) addSessionPlayer(c *conn, streamKey string) (*player, error) {
	value, ok := b.sessionMap.Load(streamKey)
	if !ok {
		return nil, errors.Errorf("session not exists, streamKey: %s", streamKey)
	}
	sess := value.(*session)

	player, err := newPlayer(
		withPlayerConn(c),
		withPlayerSession(sess),
		withPlayerPacketBufSize(150),                 //TODO: config
		withMergeWriteWaitTime(350*time.Millisecond), //TODO:config
	)
	if err != nil {
		return nil, errors.Wrap(err, "create player")
	}

	sess.addPlayer(player)

	return player, nil
}

/*
func (b *broker) getSessionTotalNumber() int32 {
	return atomic.LoadInt32(&b.sessionTotal)
}
*/

func newBroker(opts ...brokerOption) (*broker, error) {
	return (&broker{}).loadOptions(opts...)
}

func (b *broker) loadOptions(opts ...brokerOption) (*broker, error) {
	for _, opt := range opts {
		opt(b)
	}

	if b.server == nil {
		return nil, errBrokerServer
	}

	if b.demuxer == nil {
		b.demuxer = flv.NewDemuxer()
	}

	return b, nil
}

type brokerOption func(*broker)

func WithBrokerServer(server *Server) brokerOption {
	return func(b *broker) {
		b.server = server
	}
}

var (
	errBrokerServer = errors.New("broker belongs to server required")
)
