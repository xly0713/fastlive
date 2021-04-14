package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gwuhaolin/livego/protocol/amf"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"fastlive/pkg/av"
	"fastlive/pkg/rtmp/chunk"
)

type player struct {
	c       *conn
	session *session

	packetBufSize int             // packet队列大小
	packetBuffer  chan *av.Packet // packet队列
	bufferClosed  uint32
	bufferMutex   sync.Mutex

	basicTimestamp      uint32
	basicAudioTimestamp uint32
	basicVideoTimestamp uint32

	msgCount   int           // 缓冲区有几个message需要发送
	bytesCount int           // 缓冲区待发送字节数
	mwWaitTime time.Duration // 合并发送等待时间
	lastMwTime time.Time     // 前一次flush时间
}

func (p *player) doPlaying() error {
	defer p.session.delPlayer(p)

	if err := p.sendAvMetaPacket(); err != nil {
		return errors.Wrap(err, "send meta/audio/video packet")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan error)
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				msg, err := p.c.Connection.RecvIntegralMessage()
				if err != nil {
					ch <- errors.Wrap(err, "recv intergral message")
					return
				}

				if err := p.c.onRecvIntegralMessage(msg); err != nil {
					ch <- errors.Wrap(err, "on recv intergral message")
					return
				}
			}
		}
	}(ctx)

	for {
		select {
		case err := <-ch:
			return err
		default:
			if err := p.flushAvPacket(); err != nil {
				return errors.Wrap(err, "flush av packet to player")
			}

			avPacket, ok := <-p.packetBuffer
			if avPacket == nil && !ok {
				return errors.New("channel closed by writer")
			}

			if avPacket != nil {
				//fmt.Printf("play avPacket: %#v\n", avPacket)
				if err := p.sendAvPacket(avPacket); err != nil {
					return errors.Wrap(err, "sent av packet to player")
				}
			}
		}
	}
}

func (p *player) sendAvMetaPacket() error {
	if p.session.metaData != nil {
		if err := p.sendAvPacket(p.session.metaData); err != nil {
			return errors.Wrap(err, "send onMeta packet to player")
		}
	}

	if p.session.audioSeqHeader != nil {
		if err := p.sendAvPacket(p.session.audioSeqHeader); err != nil {
			return errors.Wrap(err, "send audio sequence header to player")
		}
	}

	if p.session.videoSeqHeader != nil {
		if err := p.sendAvPacket(p.session.videoSeqHeader); err != nil {
			return errors.Wrap(err, "send video sequence header to player")
		}
	}

	return nil
}

func (p *player) sendAvPacket(avPacket *av.Packet) error {
	csid := uint32(0)
	var messageTypeId chunk.RtmpMessageTypeID
	switch avPacket.PacketType {
	case av.AudioType:
		csid = 4
		messageTypeId = chunk.MsgAudioMessage
	case av.VideoType:
		csid = 6
		messageTypeId = chunk.MsgVideoMessage
	case av.MetaData:
		csid = 6
		messageTypeId = chunk.MSGAMF0DataMessage
	}

	timestamp := avPacket.Timestamp
	baseTimestamp := p.getBaseTimestamp()
	if timestamp < baseTimestamp {
		timestamp = baseTimestamp + 40 //40ms
	}

	msg := p.c.Connection.NewChunkPool.Get().(*chunk.Chunk)
	defer p.c.Connection.NewChunkPool.Put(msg)
	{
		// msg basic header
		msg.Fmt = 0
		msg.Csid = csid

		// msg message header
		msg.Timestamp = timestamp
		msg.MessageLength = uint32(len(avPacket.Data))
		msg.MessageTypeID = messageTypeId
		msg.MessageStreamID = avPacket.StreamID

		// TODO: if timestamp >= 0xffffff

		// msg body
		msg.ChunkData = avPacket.Data
	}

	if avPacket.PacketType == av.MetaData {
		var err error
		if msg.ChunkData, err = amf.MetaDataReform(msg.GetChunkData(), amf.DEL); err != nil {
			return errors.Wrap(err, "amf encode metaData")
		} else {
			chunk.WithChunkMessageLength(uint32(len(msg.ChunkData)))(msg)
		}
	}

	p.updateBaseTimestamp(msg.GetChunkMessageTypeID(), msg.GetChunkTimestamp())

	if nw, err := p.c.Connection.SendIntegralMessage(msg); err != nil {
		return errors.Wrap(err, "write chunk message")
	} else {
		p.bytesCount += nw
		p.msgCount++
	}

	return nil
}

func (p *player) flushAvPacket() error {
	if p.msgCount <= 0 || time.Since(p.lastMwTime) < p.mwWaitTime {
		return nil
	}

	nf, err := p.c.Connection.Flush()
	if err != nil {
		return errors.Wrap(err, "flush merged chunk message data")
	}

	if nf != int64(p.bytesCount) {
		return errors.Errorf("need write %d, actual: %d", p.bytesCount, nf)
	}

	p.c.server.logger.Debug("merge write message",
		zap.Int("count", p.msgCount),
		zap.Duration("ms", p.mwWaitTime),
		zap.Int("bytes", p.bytesCount))

	p.lastMwTime = time.Now()
	p.msgCount = 0
	p.bytesCount = 0

	return nil
}

func (p *player) buffPackets(avPacket *av.Packet) {
	p.bufferMutex.Lock()
	defer p.bufferMutex.Unlock()

	if atomic.LoadUint32(&p.bufferClosed) == 1 {
		return
	}

	p.packetBuffer <- avPacket //TODO: 丢帧处理
}

func (p *player) closePacketBuffer() {
	p.bufferMutex.Lock()
	defer p.bufferMutex.Unlock()

	if atomic.LoadUint32(&p.bufferClosed) == 1 {
		return
	}

	p.bufferClosed++
	close(p.packetBuffer)
}

func (p *player) updateBaseTimestamp(messageTypeId chunk.RtmpMessageTypeID, timestamp uint32) {
	switch messageTypeId {
	case chunk.MsgAudioMessage:
		p.basicAudioTimestamp = timestamp
	case chunk.MsgVideoMessage:
		p.basicVideoTimestamp = timestamp
	}

	// 保持流(audio/video)整体时间戳单增
	if p.basicAudioTimestamp > p.basicVideoTimestamp {
		p.basicTimestamp = p.basicAudioTimestamp
	} else {
		p.basicTimestamp = p.basicVideoTimestamp
	}
}

func (p *player) getBaseTimestamp() uint32 {
	return p.basicTimestamp
}

func newPlayer(opts ...playerOption) (*player, error) {
	return (&player{}).loadOptions(opts...)
}

var (
	errPlayerConn = errors.New("player conn require")
)

func (p *player) loadOptions(opts ...playerOption) (*player, error) {
	for _, opt := range opts {
		opt(p)
	}

	if p.c == nil {
		return nil, errPlayerConn
	}

	if p.packetBufSize <= 0 {
		p.packetBufSize = 10 //TODO: 更合理的默认值？
	}

	p.packetBuffer = make(chan *av.Packet, p.packetBufSize)

	if p.mwWaitTime <= 0 {
		p.mwWaitTime = 350 * time.Millisecond //默认:250ms
	}

	return p, nil
}

type playerOption func(*player)

func withPlayerConn(c *conn) playerOption {
	return func(p *player) {
		p.c = c
	}
}

func withPlayerSession(s *session) playerOption {
	return func(p *player) {
		p.session = s
	}
}

func withPlayerPacketBufSize(size int) playerOption {
	return func(p *player) {
		p.packetBufSize = size
	}
}

func withMergeWriteWaitTime(d time.Duration) playerOption {
	return func(p *player) {
		p.mwWaitTime = d
	}
}
