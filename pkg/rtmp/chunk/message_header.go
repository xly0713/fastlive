package chunk

import "fastlive/pkg/rtmp/common"

type messageHeader struct {
	Timestamp       uint32
	MessageLength   uint32
	MessageTypeID   RtmpMessageTypeID
	MessageStreamID uint32
}

func NewMessageHeader(opts ...messageHeaderOption) (*messageHeader, error) {
	return (&messageHeader{}).loadOptions(opts...)
}

func (mh *messageHeader) loadOptions(opts ...messageHeaderOption) (*messageHeader, error) {
	for _, opt := range opts {
		opt(mh)
	}

	// 3个字节编码，如果bit全部为1(即0xffffff), 则表示存在extended timestamp
	// fmt=0: 表示绝对时间戳
	// fmt=1/2: 表示相对于同一message的前一个chunk的时间戳
	if mh.Timestamp > 0xffffff {
		mh.Timestamp = 0xffffff
	}

	// mh.messageLength: fmt=0/1时，才需要编解码
	// mh.messageTypeID: fmt=0/1时，才需要编解码
	// mh.messageStreamID: fmt=0时，才需要编解码(小端序)

	return mh, nil
}

func (mh *messageHeader) encode(fmt uint8, p []byte) (size int, err error) {
	if fmt > 3 {
		return 0, errFmt
	} else if fmt == 3 {
		return 0, nil
	}

	// fmt = 0, 1, 2
	size = common.FmtToMessageHdrSize[fmt]
	if fmt <= 2 {
		// encode timestamp (delta)
		if mh.Timestamp >= 0xffffff {
			common.UintAsBytes(0xffffff, p[0:3], true)
		} else {
			common.UintAsBytes(mh.Timestamp, p[0:3], true)
		}

		if fmt <= 1 {
			// encode message length
			common.UintAsBytes(mh.MessageLength, p[3:6], true)

			// encode message type id
			common.UintAsBytes(uint32(mh.MessageTypeID), p[6:7], true)

			if fmt == 0 {
				// encode message stream id
				common.UintAsBytes(mh.MessageStreamID, p[7:11], false)
			}
		}
	}

	return size, nil
}

type messageHeaderOption func(*messageHeader)

func WithChunkMessageHeaderTimestamp(timestamp uint32) messageHeaderOption {
	return func(mh *messageHeader) {
		mh.Timestamp = timestamp
	}
}

func WithChunkMessageHeaderMessageLength(messageLength uint32) messageHeaderOption {
	return func(mh *messageHeader) {
		mh.MessageLength = messageLength
	}
}

func WithChunkMessageHeaderMessageTypeID(messageTypeID RtmpMessageTypeID) messageHeaderOption {
	return func(mh *messageHeader) {
		mh.MessageTypeID = messageTypeID
	}
}

func WithChunkMessageHeaderMessageStreamID(streamID uint32) messageHeaderOption {
	return func(mh *messageHeader) {
		mh.MessageStreamID = streamID
	}
}
