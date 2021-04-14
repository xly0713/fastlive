package flv

import (
	"fastlive/pkg/av"

	"github.com/pkg/errors"
)

type Demuxer struct{}

func NewDemuxer() *Demuxer {
	return &Demuxer{}
}

func (d *Demuxer) DecodeHeader(pkt *av.Packet) error {
	tag := new(Tag)
	_, err := tag.DecodeMediaTagHeader(pkt.Data, pkt.PacketType)
	if err != nil {
		return errors.Wrap(err, "decode media tag header")
	} else {
		pkt.PacketHeader = tag
	}

	return nil
}
