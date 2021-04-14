package chunk

import (
	"github.com/pkg/errors"
)

type header struct {
	basicHeader
	messageHeader
	ExtendedTimestamp uint32
}

func NewHeader(opts ...headerOption) (*header, error) {
	return (&header{}).loadOptions(opts...)
}

func (h *header) loadOptions(opts ...headerOption) (*header, error) {
	for _, opt := range opts {
		opt(h)
	}

	//TODO: 检查约束

	return h, nil
}

func (h *header) Encode(p []byte) (int, error) {
	basicHdrSize, err := h.basicHeader.encode(p)
	if err != nil {
		return 0, errors.Wrap(err, "encode basic header")
	}

	messageHdrSize, err := h.messageHeader.encode(h.basicHeader.Fmt, p[basicHdrSize:])
	if err != nil {
		return 0, errors.Wrap(err, "encode message header")
	}

	return basicHdrSize + messageHdrSize, nil
}

type headerOption func(*header)

func WithHeaderBasicHeader(bh *basicHeader) headerOption {
	return func(h *header) {
		h.basicHeader = *bh
	}
}

func WithHeaderMessageHeader(mh *messageHeader) headerOption {
	return func(h *header) {
		h.messageHeader = *mh
	}
}

func WithHeaderExtendedTimestamp(extendedTimestamp uint32) headerOption {
	return func(h *header) {
		h.ExtendedTimestamp = extendedTimestamp
	}
}
