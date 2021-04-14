package chunk

type Chunk struct {
	header
	ChunkData []byte
}

func New(opts ...chunkOpt) *Chunk {
	return new(Chunk).loadOptions(opts...)
}

func (c *Chunk) loadOptions(opts ...chunkOpt) *Chunk {
	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Chunk) GetChunkFmt() uint8 {
	return c.header.basicHeader.Fmt
}

func (c *Chunk) GetChunkCsid() uint32 {
	return c.header.basicHeader.Csid
}

func (c *Chunk) GetChunkTimestamp() uint32 {
	return c.header.messageHeader.Timestamp
}

func (c *Chunk) GetChunkMessageLength() uint32 {
	return c.header.messageHeader.MessageLength
}

func (c *Chunk) GetChunkMessageTypeID() RtmpMessageTypeID {
	return c.header.messageHeader.MessageTypeID
}

func (c *Chunk) GetChunkMessageStreamID() uint32 {
	return c.header.messageHeader.MessageStreamID
}

func (c *Chunk) GetChunkExtendTimestamp() uint32 {
	return c.header.ExtendedTimestamp
}

func (c *Chunk) GetChunkHeader() *header {
	return &c.header
}

func (c *Chunk) GetChunkData() []byte {
	return c.ChunkData
}

type chunkOpt func(c *Chunk)

func WithChunkFmt(fmt uint8) chunkOpt {
	return func(c *Chunk) {
		c.header.basicHeader.Fmt = fmt
	}
}

func WithChunkCsid(csid uint32) chunkOpt {
	return func(c *Chunk) {
		c.header.basicHeader.Csid = csid
	}
}

func WithChunkTimestamp(timestamp uint32) chunkOpt {
	return func(c *Chunk) {
		c.header.messageHeader.Timestamp = timestamp
	}
}

func WithChunkMessageLength(length uint32) chunkOpt {
	return func(c *Chunk) {
		c.header.messageHeader.MessageLength = length
	}
}

func WithChunkMessageTypeID(typeId RtmpMessageTypeID) chunkOpt {
	return func(c *Chunk) {
		c.header.messageHeader.MessageTypeID = typeId
	}
}

func WithChunkMessageStreamID(streamId uint32) chunkOpt {
	return func(c *Chunk) {
		c.header.messageHeader.MessageStreamID = streamId
	}
}

func WithChunkExtendedTimestamp(extendedTimestamp uint32) chunkOpt {
	return func(c *Chunk) {
		c.header.ExtendedTimestamp = extendedTimestamp
	}
}

func WithChunkData(data []byte) chunkOpt {
	return func(c *Chunk) {
		c.ChunkData = data
	}
}
