package connection

type ackWindowSize struct {
	WindowSize     uint32
	NBytes         uint32
	SequenceNumber uint32
}
