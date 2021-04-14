package chunk

import (
	"bytes"

	"github.com/gwuhaolin/livego/protocol/amf"
	"github.com/pkg/errors"

	"fastlive/pkg/rtmp/common"
)

/*
协议规定
1. RTMP Chunk Streams uses message type IDS 1,2,3,5 and 6 for protocol control messages
2. message stream ID is 0 (known as the control stream)
3. chunk stream ID (csid) is 2
4. 收到这种message，需立即处理，并忽略timestamp
*/
func NewProcotolControlMessage(typeId RtmpMessageTypeID, length uint32, value uint32) (*Chunk, error) {
	if length < 4 {
		return nil, errProcotolControlMessageLength
	}

	switch typeId {
	case MsgSetChunkSize,
		MsgAbortMessage,
		MsgAcknowledgement,
		MsgWindowAcknowledgementSize,
		MsgSetPeerBandwidth:
		goto END
	default:
		return nil, errProcotolControlMessageTypeID
	}

END:
	msg := New(
		WithChunkFmt(0),
		WithChunkCsid(2),
		WithChunkTimestamp(0),
		WithChunkMessageLength(length),
		WithChunkMessageTypeID(typeId),
		WithChunkMessageStreamID(0),
		WithChunkData(make([]byte, length)), // length >= 4 TODO: pool化
	)

	common.UintAsBytes(value, msg.ChunkData[:4], true)

	return msg, nil
}

func NewlUserControlMessage(eventType, buflen uint32) (*Chunk, error) {
	if buflen < 6 {
		return nil, errUserControlMessageLength
	}

	msg := New(
		WithChunkFmt(0),
		WithChunkCsid(2),
		WithChunkTimestamp(0),
		WithChunkMessageLength(buflen),
		WithChunkMessageTypeID(MsgUserControlMessage),
		WithChunkMessageStreamID(1),
		WithChunkData(make([]byte, buflen)), // length >= 4 TODO: pool化
	)

	msg.ChunkData[0] = byte(eventType >> 8 & 0xff)
	msg.ChunkData[1] = byte(eventType & 0xff)
	common.UintAsBytes(1, msg.ChunkData[2:6], true)

	return msg, nil
}

// command message (20/17, 使用20)
func NewCommandMessage(amfEncoder *amf.Encoder, csid, streamId uint32, args ...interface{}) (*Chunk, error) {
	buffer := bytes.NewBuffer([]byte{})
	for _, v := range args {
		if _, err := amfEncoder.Encode(buffer, v, amf.AMF0); err != nil {
			return nil, errors.Wrapf(err, "amf encode value: %v", v)
		}
	}
	data := buffer.Bytes()

	msg := New(
		WithChunkFmt(0),
		WithChunkCsid(csid),
		WithChunkTimestamp(0),
		WithChunkMessageLength(uint32(len(data))),
		WithChunkMessageTypeID(MsgAMF0CommandMessage),
		WithChunkMessageStreamID(streamId),
		WithChunkData(data),
	)

	return msg, nil
}

/*
// 此类消息不可缓冲，需立即发送
func (m *Stream) WriteProtocolCommandMessageTo(w io.Writer, localChunkSize uint32) error {
	return m.WriteCommonMessageTo(w, localChunkSize)
}

func (m *Stream) WriteUserControlMessageTo(w io.Writer, localChunkSize uint32) error {
	return m.WriteCommonMessageTo(w, localChunkSize)
}

func (m *Stream) WriteCommandMessageTo(w io.Writer, localChunkSize uint32) error {
	return m.WriteCommonMessageTo(w, localChunkSize)
}
*/

/*
func (m *Stream) WriteCommonMessageTo(w io.Writer, localChunkSize uint32) error {
	messageLength := m.header.messageHeader.messageLength
	if messageLength < 4 {
		return errProcotolControlMessageLength
	}

	var tmpFmt uint8 = 0
	unWrite := messageLength                          // 未发送的chunk data字节数
	nChunks := len(m.ChunkData) / int(localChunkSize) //注意: 可能比实际要拆的chunk数小1

	for idx := 0; idx <= nChunks; idx++ {
		if unWrite == 0 {
			break
		}

		if idx > 0 {
			tmpFmt = 3
		}

		start := idx * int(localChunkSize)
		end := start + int(localChunkSize)
		if end > int(messageLength) {
			end = int(messageLength)
		}
		chunkSize := uint32(end - start)

		// create chunk
		chunk, err := NewStream(
			WithChunkStreamFmt(tmpFmt),
			WithChunkStreamCsid(m.header.basicHeader.csid),
			WithChunkStreamTimeStamp(m.header.messageHeader.timestamp),
			WithChunkStreamMessageLength(messageLength),
			WithChunkStreamMessageTypeID(m.header.messageHeader.messageTypeID),
			WithChunkStreamMessageStreamID(m.header.messageHeader.messageStreamID),
			WithChunkStreamChunkData(m.ChunkData[start:end]),
		)
		if err != nil {
			return errors.Wrapf(err, "create chunk according to localChunkSize: %d", localChunkSize)
		}

		// encode header
		headerBuf := make([]byte, 14)                     // TODO: pool化
		headerSize, err := chunk.header.Encode(headerBuf) //headerSize: 编码实际占用的字节数
		if err != nil {
			return errors.Wrap(err, "encode chunk header")
		} else {
			headerBuf = headerBuf[0:headerSize]
		}

		// write header
		if nw, err := w.Write(headerBuf); err != nil {
			return errors.Wrap(err, "write chunk header")
		} else {
			if nw != headerSize {
				return errors.Errorf("need write %d bytes, actual: %d", headerSize, nw)
			}
		}

		//TODO: encode and send extended timestamp

		// write chunk body
		if nw, err := w.Write(chunk.ChunkData); err != nil {
			return errors.Wrap(err, "write chunk data")
		} else {
			if nw != int(chunkSize) {
				return errors.Errorf("need write %d bytes, actual: %d", chunkSize, nw)
			}
			unWrite = messageLength - chunkSize
		}
	}

	return nil
}
*/

var (
	errProcotolControlMessageLength = errors.New("incorrect protocol control message length, require at least 4")
	errUserControlMessageLength     = errors.New("incorrect user control message length, require at least 6")
	errProcotolControlMessageTypeID = errors.New("incorrect protocol message type id")
)
