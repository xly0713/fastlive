package flv

import (
	"fastlive/pkg/av"

	"github.com/pkg/errors"
)

type flvTag struct {
	TagType   uint8  // tag类型 （1 byte）
	DataSize  uint32 // 数据长度 (3 bytes)
	Timestamp uint32 // 时间戳 （3 bytes）
	StreamID  uint32 // 流ID (3 bytes)
}

type mediaTag struct {
	SoundFormat   uint8 //音频编码格式 如：10（AAC）
	SoundRate     uint8 // 音频采样率 (0: 5.5kHZ  1: 11kHZ  2:22kHZ  3:44kHZ)
	SoundSize     uint8 // 采样大小
	SoundType     uint8 // 声道类型 mono(单声道)  stereo(立体声)
	AACPacketType uint8 // 0: sequence header   1: aac raw

	// 帧类型  1: keyframe  2: inter frame 3: disposable inter frame(h.263 only) 4: generated keyframe(server) 5: video info/command frame
	FrameType     uint8
	CodecID       uint8 // 编码ID  如：7（AVC/H.264）
	AvcPacketType uint8 // AVC编码数据类型  0: sequence header  1: NALU  2: end of sequence

	CompostioinTime int32 // 合成时间
}

type Tag struct {
	flvTag
	mediaTag
}

func (t *Tag) SoundFormat() uint8 {
	return t.mediaTag.SoundFormat
}

func (t *Tag) AACPacketType() uint8 {
	return t.mediaTag.AACPacketType
}

func (t *Tag) IsKeyFrame() bool {
	return t.mediaTag.FrameType == 1
}

func (t *Tag) IsSequenceHeader() bool {
	return t.IsKeyFrame() && t.mediaTag.AvcPacketType == 0
}

func (t *Tag) CodecID() uint8 {
	return t.mediaTag.CodecID
}

func (t *Tag) CompostioinTime() int32 {
	return t.mediaTag.CompostioinTime
}

func (t *Tag) DecodeMediaTagHeader(b []byte, typ av.AVPacketType) (n int, err error) {
	switch typ {
	case av.VideoType:
		return t.decodeVideoHeader(b)
	default:
		return t.decodeAudioHeader(b)
	}
}

func (t *Tag) decodeVideoHeader(b []byte) (n int, err error) {
	if len(b) < 5 {
		err = errors.Errorf("invalid Video Data len=%d", len(b))
		return
	}

	flags := b[0]
	t.mediaTag.FrameType = flags >> 4
	t.mediaTag.CodecID = flags & 0xf

	n = 1

	switch t.mediaTag.CodecID {
	case 7: // H.264
		switch t.mediaTag.FrameType {
		case 1, 2: // 1: key frame  2: inter frame
			t.mediaTag.AvcPacketType = b[1]
			for i := 2; i < 5; i++ {
				t.mediaTag.CompostioinTime = t.mediaTag.CompostioinTime<<8 + int32(b[i])
			}
			n += 4
		}
	}

	return
}

func (t *Tag) decodeAudioHeader(b []byte) (n int, err error) {
	if len(b) < 1 {
		err = errors.Errorf("invalid audio data len=%d", len(b))
		return
	}

	flags := b[0]
	t.mediaTag.SoundFormat = flags >> 4
	t.mediaTag.SoundRate = (flags >> 2) & 0x3
	t.mediaTag.SoundSize = (flags >> 1) & 0x1
	t.mediaTag.SoundType = flags & 0x1

	n = 1
	switch t.mediaTag.SoundFormat {
	case 10: // AAC
		t.mediaTag.AACPacketType = b[1]
		n++
	}

	return
}
