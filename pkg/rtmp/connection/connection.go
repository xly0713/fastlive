package connection

import (
	"bufio"
	"io"
	"net"
	"sync"

	"github.com/gwuhaolin/livego/protocol/amf"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"fastlive/pkg/rtmp/chunk"
	"fastlive/pkg/rtmp/common"
)

type Connection struct {
	Rwc    net.Conn
	Logger *zap.Logger

	// read
	ReadBufSize     int
	Reader          *bufio.Reader
	RemoteChunkSize uint32
	InAckSize       ackWindowSize
	AmfDecoder      *amf.Decoder

	// write
	LocalChunkSize uint32
	OutAckSize     ackWindowSize
	AmfEncoder     *amf.Encoder
	writeBuf       net.Buffers

	TransactionID int
	messages      map[uint32]*chunk.Stream

	DecodeHdrPool *sync.Pool
	EncodeHdrPool *sync.Pool
	NewChunkPool  *sync.Pool
}

var (
	errRawConnection   = errors.New("raw connection required")
	errReadHdrPoll     = errors.New("read header pool required")
	errChunkEncodePool = errors.New("chunk encode pool required")
	errNewChunkPool    = errors.New("new chunk pool required")
)

func (c *Connection) Init() error {
	if c.Rwc == nil {
		return errRawConnection
	}

	if c.DecodeHdrPool == nil {
		return errReadHdrPoll
	}

	if c.EncodeHdrPool == nil {
		return errChunkEncodePool
	}

	if c.NewChunkPool == nil {
		return errNewChunkPool
	}

	if c.ReadBufSize > 0 {
		c.Reader = bufio.NewReaderSize(c.Rwc, c.ReadBufSize)
	} else {
		c.Reader = bufio.NewReader(c.Rwc)
	}

	if c.RemoteChunkSize < 128 {
		c.RemoteChunkSize = 128
	}

	if c.AmfDecoder == nil {
		c.AmfDecoder = amf.NewDecoder()
	}

	if c.LocalChunkSize <= 128 {
		c.LocalChunkSize = 4096
	}

	if c.OutAckSize.WindowSize <= 0 {
		c.OutAckSize.WindowSize = 2500000
	}

	c.writeBuf = make(net.Buffers, 0, 128)

	c.messages = make(map[uint32]*chunk.Stream)

	return nil
}

func (c *Connection) Read(p []byte) (n int, err error) {
	n, err = io.ReadAtLeast(c.Reader, p, len(p))
	if err != nil {
		if err == io.EOF { // peer close
			c.Rwc.Close()
		}

		return n, err
	}

	//TODO: 记录inAck信息
	return n, nil
}

func (c *Connection) Write(p []byte) (n int, err error) {
	c.writeBuf = append(c.writeBuf, p)
	return len(p), nil
}

func (c *Connection) Flush() (n int64, err error) {
	if len(c.writeBuf) <= 0 {
		return 0, nil
	}

	// header(0), chunkBody(1), header(2), chunkBody(3) ...
	for idx, p := range c.writeBuf {
		if idx&0x01 != 0 {
			continue
		}
		defer func(b []byte) {
			c.EncodeHdrPool.Put(b)
		}(p)
	}

	//TODO: 记录outAck信息
	nw, err := c.writeBuf.WriteTo(c.Rwc)
	if cap(c.writeBuf) < 64 {
		c.writeBuf = make(net.Buffers, 0, 128)
	}

	return nw, err
}

func (c *Connection) Close() error {
	return c.Rwc.Close()
}

func (c *Connection) Peek(n int) ([]byte, error) {
	return c.Reader.Peek(n)
}

func (c *Connection) Discard(n int) (discarded int, err error) {
	return c.Reader.Discard(n)
}

func (c *Connection) RecvIntegralMessage() (*chunk.Stream, error) {
	//basicHdrBuf := make([]byte, 3) // TODO: pool化, 最多3个字节
	tmpHeaderBuf := c.DecodeHdrPool.Get().([]byte)
	defer func() {
		c.DecodeHdrPool.Put(tmpHeaderBuf) //放回pool
	}()

	for {
		tmpFmt, csid, err := c.readChunkBasicHeader(tmpHeaderBuf[0:3])
		if err != nil {
			return nil, errors.Wrap(err, "read chunk basic header")
		}
		c.Logger.Debug("read basic header",
			zap.Uint8("fmt", tmpFmt),
			zap.Uint32("csid", csid))

		msg, cached := c.messages[csid]
		if !cached {
			msg, _ = chunk.NewStream(
				chunk.WithChunkStreamFmt(tmpFmt),
				chunk.WithChunkStreamCsid(csid),
				chunk.WithChunkStreamFirstChunk(true),
			)
			c.messages[csid] = msg
		}

		messageHdr := tmpHeaderBuf[0:common.FmtToMessageHdrSize[tmpFmt]]
		if err := c.readChunkMessageHeader(msg, tmpFmt, messageHdr); err != nil {
			return nil, errors.Wrap(err, "read chunk message header")
		}

		c.Logger.Debug("read message header",
			zap.Uint8("fmt", tmpFmt),
			zap.Uint32("ext_time", msg.ExtendedTimestamp),
			zap.Uint32("time", msg.Timestamp),
			zap.Uint32("length", msg.MessageLength),
			zap.Uint32("streamId", msg.MessageStreamID),
		)

		if err := c.readChunkMessageBody(msg); err != nil {
			return nil, errors.Wrap(err, "read chunk message body")
		}

		if msg.IsIntergral { //接收到完整的message
			return msg, nil
		}
	}
}

func (c *Connection) readChunkBasicHeader(basicHdrBuf []byte) (uint8, uint32, error) {
	h, err := common.ReadBytesAsUint32(c, basicHdrBuf[0:1], true)
	if err != nil {
		return 0, 0, errors.Wrap(err, "read the 1st byte of chunk basic header")
	}

	fmt := uint8(h >> 6)
	csid := h & 0x3f

	switch csid {
	case 0, 1:
		id, err := common.ReadBytesAsUint32(c, basicHdrBuf[1:2+csid], false)
		if err != nil {
			return 0, 0, errors.Wrapf(err, "read the next %d bytes of chunk basic header", 1+csid)
		}
		csid = id + 64
		return fmt, csid, nil
	default:
		return fmt, csid, nil
	}
}

func (c *Connection) readChunkMessageHeader(msg *chunk.Stream, tmpFmt uint8, messageHdrbuf []byte) error {
	if msg.FirstChunk && tmpFmt > 0 {
		csid := msg.GetChunkCsid()
		if csid == 2 && tmpFmt == 1 { // 首个chunk是协议用户控制消息
			c.Logger.Info("accept csid=2, fmt=1 to adapt to librtmp")
		} else {
			return errors.Errorf("init chunk stream now, fmt 1 required, but actual fmt=%d, csid=%d", tmpFmt, csid)
		}
	}

	// 一个message只读了部分chunk，而fmt=0标志着新的message的开始, 返回错误
	if msg.HasRead > 0 && tmpFmt == 0 {
		return errors.Errorf("previous chunk message not integral, but fmt=0")
	}

	messageHdrSize := len(messageHdrbuf)
	if _, err := c.Read(messageHdrbuf); err != nil {
		return errors.Wrapf(err, "read %d bytes message header", messageHdrSize)
	}

	/* parse the message header
	 * 3 bytes: timestamp (delta)    fmt=0,1,2
	 * 3 bytes: payload length       fmt=0,1
	 * 1 bytes: message type         fmt=0,1
	 * 4 bytes: stream id            fmt=0
	 */

	if tmpFmt <= 2 {
		timestamp := common.BytesAsUint32(messageHdrbuf[0:3], true) // timestamp (delta)
		msg.TimestampDelta = timestamp
		if timestamp >= 0xffffff {
			msg.IsExtendedTimestamp = true
		}

		if !msg.IsExtendedTimestamp {
			switch tmpFmt {
			case 0:
				msg.Timestamp = timestamp
			default: // 1 or 2
				msg.Timestamp += timestamp
			}
		}

		if tmpFmt <= 1 {
			payloadLength := common.BytesAsUint32(messageHdrbuf[3:6], true)
			if msg.HasRead > 0 && msg.GetChunkMessageLength() != payloadLength {
				return errors.Errorf("message length mismatch while read partial chunk message, pre: %d, this chunk: %d",
					msg.GetChunkMessageLength(), payloadLength)
			}

			msg.MessageLength = payloadLength
			msg.MessageTypeID = chunk.RtmpMessageTypeID(common.BytesAsUint32(messageHdrbuf[6:7], true))

			if tmpFmt == 0 {
				streamId := common.BytesAsUint32(messageHdrbuf[7:11], false)
				msg.MessageStreamID = streamId
				c.Logger.Debug("basic and message header decode completed",
					zap.Uint8("fmt", tmpFmt),
					zap.Int("messageHdrSize", messageHdrSize),
					zap.Uint32("ext_time", msg.ExtendedTimestamp),
					zap.Uint32("time", msg.Timestamp),
					zap.Uint32("length", msg.MessageLength),
					zap.Any("typeId", msg.MessageTypeID),
					zap.Uint32("streamId", msg.MessageStreamID))
			} else { // tmpFmt = 1
				c.Logger.Debug("basic and message header decode completed",
					zap.Uint8("fmt", tmpFmt),
					zap.Int("messageHdrSize", messageHdrSize),
					zap.Uint32("ext_time", msg.ExtendedTimestamp),
					zap.Uint32("time", msg.Timestamp),
					zap.Uint32("length", msg.MessageLength),
					zap.Any("typeId", msg.MessageTypeID))
			}
		} else { // tmpFmt = 2
			c.Logger.Debug("basic and message header decode completed",
				zap.Uint8("fmt", tmpFmt),
				zap.Int("messageHdrSize", messageHdrSize),
				zap.Uint32("ext_time", msg.ExtendedTimestamp),
				zap.Uint32("time", msg.Timestamp))
		}
	} else { // tmpFmt = 3
		if msg.FirstChunk && !msg.IsExtendedTimestamp {
			msg.Timestamp += msg.TimestampDelta
		}
		c.Logger.Debug("basic and message header decode completed",
			zap.Uint8("fmt", tmpFmt),
			zap.Int("messageHdrSize", messageHdrSize),
			zap.Uint32("ext_time", msg.ExtendedTimestamp))
	}

	// read extended timestamp
	if msg.IsExtendedTimestamp {
		buffer, err := c.Peek(4)
		if err != nil {
			return errors.Wrap(err, "peek 4 bytes")
		}
		extendedTimestamp := common.BytesAsUint32(buffer, true)
		extendedTimestamp &= 0x7fffffff

		chunkTimestamp := msg.GetChunkTimestamp()
		if !msg.FirstChunk && chunkTimestamp > 0 && chunkTimestamp != extendedTimestamp {
			c.Logger.Warn("no 4 bytes extended timestamp in the continued chunk")
		} else {
			_, _ = c.Discard(4)
			msg.Timestamp = extendedTimestamp
		}

		c.Logger.Debug("read extended extentedTimestamp completed", zap.Uint32("time", msg.Timestamp))
	}

	timestamp := msg.GetChunkTimestamp()
	timestamp &= 0x7fffffff
	chunk.WithChunkStreamTimeStamp(timestamp)

	// check message length

	msg.FirstChunk = false

	return nil
}

func (c *Connection) readChunkMessageBody(msg *chunk.Stream) error {
	messageLength := msg.GetChunkMessageLength()
	if messageLength == 0 {
		c.Logger.Warn("get an empty RTMP message",
			zap.Any("typeId", msg.MessageTypeID),
			zap.Uint32("length", msg.MessageLength),
			zap.Uint32("time", msg.Timestamp),
			zap.Uint32("streamId", msg.MessageStreamID))

		msg.HasRead = 0
		msg.ChunkData = nil

		return nil
	}

	size := messageLength - msg.HasRead
	if size > c.RemoteChunkSize {
		size = c.RemoteChunkSize
	}
	c.Logger.Debug("read chunk payload",
		zap.Uint32("size", size),
		zap.Uint32("messageLength", messageLength),
		zap.Uint32("received_size", msg.HasRead),
		zap.Uint32("remoteChunkSize", c.RemoteChunkSize))

	if msg.HasRead == 0 {
		msg.ChunkData = make([]byte, messageLength)
	}

	buf := msg.ChunkData[msg.HasRead : msg.HasRead+size]
	if _, err := c.Read(buf); err != nil {
		return errors.Wrapf(err, "read %d bytes chunk body", size)
	} else {
		msg.HasRead += size
	}

	if msg.HasRead == messageLength {
		msg.IsIntergral = true
		c.Logger.Debug("get intergral RTMP message",
			zap.Any("typeId", msg.MessageTypeID),
			zap.Uint32("length", msg.MessageLength),
			zap.Uint32("time", msg.Timestamp),
			zap.Uint32("streamId", msg.MessageStreamID))
	} else {
		c.Logger.Debug("get partial RTMP message",
			zap.Any("typeId", msg.MessageTypeID),
			zap.Uint32("length", msg.MessageLength),
			zap.Uint32("time", msg.Timestamp),
			zap.Uint32("streamId", msg.MessageStreamID),
			zap.Uint32("partialSize", msg.HasRead))
	}

	return nil
}

func (c *Connection) SendIntegralMessage(msg *chunk.Chunk) (int, error) {
	messageLength := msg.GetChunkMessageLength()
	if messageLength <= 0 {
		return 0, errors.New("message length <= 0")
	}

	writeSize := 0

	var tmpFmt uint8 = 0
	unWrite := messageLength                              // 未发送的chunk data字节数
	nChunks := len(msg.ChunkData) / int(c.LocalChunkSize) //注意: 可能比实际要拆的chunk数小1

	ck := c.NewChunkPool.Get().(*chunk.Chunk)
	defer c.NewChunkPool.Put(ck)

	for idx := 0; idx <= nChunks; idx++ {
		if unWrite == 0 {
			break
		}

		if idx > 0 {
			tmpFmt = 3
		}

		start := idx * int(c.LocalChunkSize)
		end := start + int(c.LocalChunkSize)
		if end > int(messageLength) {
			end = int(messageLength)
		}
		chunkSize := uint32(end - start)

		// create chunk
		{
			// basic header
			ck.Fmt = tmpFmt
			ck.Csid = msg.Csid

			// message header
			ck.Timestamp = msg.Timestamp
			ck.MessageLength = messageLength
			ck.MessageTypeID = msg.MessageTypeID
			ck.MessageStreamID = msg.MessageStreamID

			// extended timestamp
			ck.ExtendedTimestamp = msg.ExtendedTimestamp

			// chunk data
			ck.ChunkData = msg.ChunkData[start:end]
		}

		// encode header
		headerBuf := c.EncodeHdrPool.Get().([]byte) //connection.Flush()时需重置并返回pool中
		headerSize, err := ck.GetChunkHeader().Encode(headerBuf)
		if err != nil {
			return 0, errors.Wrap(err, "encode chunk header")
		}

		// write header
		if nw, err := c.Write(headerBuf[:headerSize]); err != nil {
			return 0, errors.Wrap(err, "write chunk header")
		} else {
			writeSize += nw
		}

		//TODO: encode and send extended timestamp

		// write chunk body
		if nw, err := c.Write(ck.ChunkData); err != nil {
			return 0, errors.Wrap(err, "write chunk data")
		} else {
			writeSize += nw
			unWrite = messageLength - chunkSize
		}
	}

	return writeSize, nil
}

func (c *Connection) ResponseAcknowledgementMessage() error {
	// TODO:
	return nil
}

func (c *Connection) HandleSetChunkSizeMessage(msg *chunk.Stream) {
	c.RemoteChunkSize = common.BytesAsUint32(msg.ChunkData, true)
}

func (c *Connection) HandleAcknowledgementMessage(msg *chunk.Stream) {
	//TODO:
}

func (c *Connection) HandleWindowAcknowledgementSizeMessage(msg *chunk.Stream) {
}
