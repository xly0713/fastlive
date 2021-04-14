package av

type PacketHeader interface{}

type AVPacketType uint32

const (
	_         AVPacketType = iota
	AudioType              = 0x08
	VideoType              = 0x09
	MetaData               = 0x12
)

type Packet struct {
	PacketHeader
	Data []byte

	Timestamp uint32 // dts
	StreamID  uint32

	PacketType AVPacketType
}

func NewPacket(opts ...packetOption) *Packet {
	return (&Packet{}).loadOptions(opts...)
}

func (p *Packet) loadOptions(opts ...packetOption) *Packet {
	for _, opt := range opts {
		opt(p)
	}

	return p
}

type packetOption func(*Packet)

func WithPacketData(data []byte) packetOption {
	return func(p *Packet) {
		p.Data = data // no copy
	}
}

func WithPacketType(typ AVPacketType) packetOption {
	return func(p *Packet) {
		p.PacketType = typ
	}
}

func WithPacketTimestamp(timestamp uint32) packetOption {
	return func(p *Packet) {
		p.Timestamp = timestamp
	}
}

func WithPacketStreamID(streamId uint32) packetOption {
	return func(p *Packet) {
		p.StreamID = streamId
	}
}
