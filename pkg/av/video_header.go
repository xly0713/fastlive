package av

type VideoPacketHeader interface {
	PacketHeader
	IsKeyFrame() bool       //是否关键帧
	IsSequenceHeader() bool // 是否Seq
	CodecID() uint8         //编码id
	CompostioinTime() int32 //合成时间
}
