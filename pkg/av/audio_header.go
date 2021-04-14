package av

type AudioPacketHeader interface {
	PacketHeader
	SoundFormat() uint8
	AACPacketType() uint8
}
