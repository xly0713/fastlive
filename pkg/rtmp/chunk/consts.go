package chunk

type RtmpMessageTypeID uint32

const (
	_                             RtmpMessageTypeID = iota
	MsgSetChunkSize                                            //0x01
	MsgAbortMessage                                            //0x02
	MsgAcknowledgement                                         //0x03
	MsgUserControlMessage                                      //0x04
	MsgWindowAcknowledgementSize                               //0x05
	MsgSetPeerBandwidth                                        //0x06
	MsgEdgeAndOriginServerCommand                              //0x07(internal, protocol not define)
	MsgAudioMessage                                            //0x08
	MsgVideoMessage                                            //0x09
	MsgAMF3DataMessage            RtmpMessageTypeID = 5 + iota //0x0F
	MsgAMF3SharedObject                                        //0x10
	MsgAMF3CommandMessage                                      //0x11
	MSGAMF0DataMessage                                         //0x12
	MSGAMF0SharedObject                                        //0x13
	MsgAMF0CommandMessage                                      //0x14
	MsgAggregateMessage           RtmpMessageTypeID = 22       //0x16
)
