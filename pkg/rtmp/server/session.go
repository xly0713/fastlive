package server

import (
	"sync"
	"time"

	"github.com/pkg/errors"

	"fastlive/pkg/av"
	"fastlive/pkg/rtmp/chunk"
)

type session struct {
	id         string //推流会话ID
	vhost      string
	appName    string
	streamName string

	publisher *conn     //收流端
	players   sync.Map  //播流端 <player地址>
	offline   chan bool //流会话下线
	broker    *broker   //session管理器
	streamKey string    //session在管理器中的索引,方便删除

	metaData       *av.Packet
	audioSeqHeader *av.Packet
	videoSeqHeader *av.Packet
}

func (s *session) onRecvAVMessage(msg *chunk.Stream, messageTypeId chunk.RtmpMessageTypeID) error {
	avPacket := new(av.Packet)

	switch messageTypeId {
	case chunk.MsgAudioMessage:
		avPacket.PacketType = av.AudioType
	case chunk.MsgVideoMessage:
		avPacket.PacketType = av.VideoType
	}

	avPacket.StreamID = msg.GetChunkMessageStreamID()
	avPacket.Data = msg.ChunkData
	avPacket.Timestamp = msg.GetChunkTimestamp()

	if err := s.broker.demuxer.DecodeHeader(avPacket); err != nil {
		return errors.Wrap(err, "decode avpacket header")
	}

	switch avPacket.PacketType {
	case av.AudioType:
		ah, ok := avPacket.PacketHeader.(av.AudioPacketHeader)
		if ok {
			if ah.SoundFormat() == 10 /* AAC */ && ah.AACPacketType() == 0 /* sequence header */ {
				s.audioSeqHeader = avPacket
			}
		}
	case av.VideoType:
		bSh := avPacket.PacketHeader.(av.VideoPacketHeader).IsSequenceHeader()
		if bSh {
			s.videoSeqHeader = avPacket
		}
	}

	// TODO: GOP

	s.fanOut(avPacket)

	return nil
}

func (s *session) onRecvDataMessage(msg *chunk.Stream, messageTypeId chunk.RtmpMessageTypeID) error {
	if s.publisher != nil {
		if err := s.publisher.handleDataMessage(msg, messageTypeId); err != nil {
			return errors.Wrap(err, "handle data message")
		}

		avPacket := new(av.Packet)
		avPacket.PacketType = av.MetaData
		avPacket.StreamID = msg.GetChunkMessageStreamID()
		avPacket.Data = msg.ChunkData
		avPacket.Timestamp = msg.GetChunkTimestamp()

		s.metaData = avPacket
		s.fanOut(avPacket)
	}

	return nil
}

func (s *session) fanOut(avPacket *av.Packet) {
	s.players.Range(func(k, v interface{}) bool {
		player := v.(*player)
		player.buffPackets(avPacket)
		return true
	})
}

func (s *session) addPlayer(player *player) *session {
	key := player.c.Rwc.RemoteAddr().String()
	s.players.Store(key, player)
	return s
}

func (s *session) delPlayer(player *player) *session {
	player.closePacketBuffer()

	key := player.c.Rwc.RemoteAddr().String()
	s.players.Delete(key)
	return s
}

func (s *session) checkOffline() {
	<-s.offline

	time.AfterFunc(30*time.Second, func() { //TODO: config
		if s.publisher != nil { //session恢复上线 TODO:mutex
			return
		}

		// clean up
		s.players.Range(func(k, v interface{}) bool {
			if player, ok := v.(*player); ok {
				s.delPlayer(player)
			}
			return true
		})

		s.broker.delSession(s.streamKey)
	})
}

func newSession(opts ...sessionOption) (*session, error) {
	return (&session{}).loadOptions(opts...)
}

func (s *session) loadOptions(opts ...sessionOption) (*session, error) {
	for _, opt := range opts {
		opt(s)
	}

	if s.id == "" {
		return nil, errSessionId
	}

	if s.vhost == "" {
		return nil, errSessionVhost
	}

	if s.appName == "" {
		return nil, errSessionAppName
	}

	if s.streamName == "" {
		return nil, errSessionStreamName
	}

	if s.publisher == nil {
		return nil, errSessionPublisher
	}

	if s.offline == nil {
		s.offline = make(chan bool, 1)
		go s.checkOffline()
	}

	if s.broker == nil {
		return nil, errSessionBroker
	}

	if s.streamKey == "" {
		return nil, errSessionStreamKey
	}

	return s, nil
}

type sessionOption func(*session)

func WithSessionId(id string) sessionOption {
	return func(s *session) {
		s.id = id
	}
}

func WithSessionVhost(vhost string) sessionOption {
	return func(s *session) {
		s.vhost = vhost
	}
}

func WithSessionAppName(appName string) sessionOption {
	return func(s *session) {
		s.appName = appName
	}
}

func WithSessionStreamName(streamName string) sessionOption {
	return func(s *session) {
		s.streamName = streamName
	}
}

func WithSessionPublisher(publisher *conn) sessionOption {
	return func(s *session) {
		s.publisher = publisher
	}
}

func WithSessionBroker(b *broker) sessionOption {
	return func(s *session) {
		s.broker = b
	}
}

func WithSessionStreamKey(sk string) sessionOption {
	return func(s *session) {
		s.streamKey = sk
	}
}

var (
	errSessionId         = errors.New("session id required")
	errSessionVhost      = errors.New("session belongs to vhost required")
	errSessionAppName    = errors.New("session belongs to appName required")
	errSessionStreamName = errors.New("session belongs to streamName required")
	errSessionStreamKey  = errors.New("session belongs to streamKey required")
	errSessionBroker     = errors.New("session belongs to broker required")
	errSessionPublisher  = errors.New("session publisher required")
)
