package chunk

import (
	"fastlive/pkg/rtmp/common"

	"github.com/pkg/errors"
)

type basicHeader struct {
	Fmt  uint8
	Csid uint32
}

func NewBasicHeader(opts ...basicHeaderOption) (*basicHeader, error) {
	return (&basicHeader{}).loadOptions(opts...)
}

func (bh *basicHeader) loadOptions(opts ...basicHeaderOption) (*basicHeader, error) {
	for _, opt := range opts {
		opt(bh)
	}

	if bh.Fmt > 3 {
		return nil, errFmt
	}

	if bh.Csid < 2 || bh.Csid > 65559 {
		return nil, errCsid
	}

	return bh, nil
}

func (bh *basicHeader) encode(p []byte) (size int, err error) {
	b1 := (bh.Fmt << 6)

	csid := bh.Csid
	switch {
	case csid < 2:
		return 0, errCsid
	case csid < 64:
		b1 |= uint8(csid)
		p[0] = b1
		return 1, nil
	case csid < 320:
		b1 |= 0
		p[0] = b1
		p[1] = byte(csid - 64)
		return 2, nil
	case csid <= 65599:
		b1 |= 1
		p[0] = b1
		common.UintAsBytes(csid-64, p[1:3], false)
		return 3, nil
	default:
		return 0, errCsid
	}
}

type basicHeaderOption func(*basicHeader)

func WithChunkBasicHeaderFmt(fmt uint8) basicHeaderOption {
	return func(bh *basicHeader) {
		bh.Fmt = fmt
	}
}

func WithChunkBasicHeaderCsid(csid uint32) basicHeaderOption {
	return func(bh *basicHeader) {
		bh.Csid = csid
	}
}

var (
	errFmt  = errors.New("fmt must in [0,1,2,3]")
	errCsid = errors.New("fmt must range from 2 to 65559")
)
