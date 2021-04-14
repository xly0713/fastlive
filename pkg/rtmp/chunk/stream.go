package chunk

type Stream struct {
	Chunk

	FirstChunk          bool // 标记是否message的第一个chunk
	HasRead             uint32
	IsIntergral         bool
	TimestampDelta      uint32
	IsExtendedTimestamp bool
}

func NewStream(opts ...streamOption) (*Stream, error) {
	return (&Stream{}).loadOptions(opts...)
}

func (s *Stream) loadOptions(opts ...streamOption) (*Stream, error) {
	for _, opt := range opts {
		opt(s)
	}

	if s.Fmt > 3 {
		return nil, errFmt
	}

	if s.Csid < 2 || s.Csid > 65559 {
		return nil, errCsid
	}

	// check firstChunk

	return s, nil
}

func (s *Stream) Reset() {
	s.HasRead = 0
	s.IsIntergral = false
}

type streamOption func(*Stream)

func WithChunkStreamFmt(fmt uint8) streamOption {
	return func(s *Stream) {
		s.header.basicHeader.Fmt = fmt
	}
}

func WithChunkStreamCsid(csid uint32) streamOption {
	return func(s *Stream) {
		s.header.basicHeader.Csid = csid
	}
}

func WithChunkStreamTimeStamp(timestamp uint32) streamOption {
	return func(s *Stream) {
		s.header.messageHeader.Timestamp = timestamp
	}
}

func WithChunkStreamMessageLength(length uint32) streamOption {
	return func(s *Stream) {
		s.header.messageHeader.MessageLength = length
	}
}

func WithChunkStreamMessageTypeID(typeId RtmpMessageTypeID) streamOption {
	return func(s *Stream) {
		s.header.messageHeader.MessageTypeID = typeId
	}
}

func WithChunkStreamMessageStreamID(streamId uint32) streamOption {
	return func(s *Stream) {
		s.header.messageHeader.MessageStreamID = streamId
	}
}

func WithChunkStreamExtendedTimestamp(extendedTimestamp uint32) streamOption {
	return func(s *Stream) {
		s.header.ExtendedTimestamp = extendedTimestamp
	}
}

func WithChunkStreamFirstChunk(firstChunk bool) streamOption {
	return func(s *Stream) {
		s.FirstChunk = firstChunk
	}
}
