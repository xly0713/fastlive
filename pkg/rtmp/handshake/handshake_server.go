package handshake

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/pkg/errors"

	"fastlive/pkg/rtmp/common"
)

func WithClient(rw io.ReadWriter, handshakeTimeout time.Duration) error {
	ch := make(chan error, 1)
	go func() {
		ch <- serverHandshke(rw)
	}()

	select {
	case err := <-ch:
		return err
	case <-time.After(handshakeTimeout):
		return errors.New("handshake timeout")
	}
}

func serverHandshke(rw io.ReadWriter) error {
	/* random:
	1. c0c1c2: c0(1) + c1(1536) + c2(1536)
	2. s0s1s2: s0(1) + s1(1536) + c2(1536)
	*/
	var random [(1 + 1536*2) * 2]byte

	c0c1c2 := random[:1536*2+1]
	c0 := c0c1c2[:1]
	c1 := c0c1c2[1 : 1536+1]
	c0c1 := c0c1c2[:1536+1]
	c2 := c0c1c2[1536+1:]

	s0s1s2 := random[1536*2+1:]
	s0 := s0s1s2[:1]
	s1 := s0s1s2[1 : 1536+1]
	s0s1 := s0s1s2[:1536+1]
	s2 := s0s1s2[1536+1:]

	// read c0c1
	if _, err := io.ReadFull(rw, c0c1); err != nil {
		return errors.Wrap(err, "read c0c1")
	}

	if c0[0] != 3 {
		return fmt.Errorf("rtmp: handshake version=%d invalid", c0[0])
	}
	s0[0] = 3

	cliTime := common.BytesAsUint32(c1[0:4], true)
	cliVer := common.BytesAsUint32(c1[4:8], true)
	if cliVer != 0 {
		var ok bool
		var digest []byte
		if ok, digest = complexHandshakeParseC1(c1, hsClientPartialKey, hsServerFullKey); !ok {
			return fmt.Errorf("rtmp: handshake server: C1 invalid")
		}

		srvTime := cliTime
		srvVer := uint32(0x0d0e0a0d)
		complexHandshakeCreateS0S1(s0s1, srvTime, srvVer, hsServerPartialKey)
		complexHandshakeCreateS2(s2, digest)
	} else {
		copy(s1, c2)
		copy(s2, c1)
	}

	// write s0s1s2
	if _, err := rw.Write(s0s1s2); err != nil {
		return errors.Wrap(err, "write s0s1s2")
	}

	// read c2
	if _, err := io.ReadFull(rw, c2); err != nil {
		return errors.Wrap(err, "read c2")
	}

	return nil
}

func complexHandshakeParseC1(p []byte, peerkey []byte, key []byte) (ok bool, digest []byte) {
	var pos int
	if pos = complexHandshakeFindDigest(p, peerkey, 772); pos == -1 {
		if pos = complexHandshakeFindDigest(p, peerkey, 8); pos == -1 {
			return
		}
	}
	ok = true
	digest = complexHandshakeMakeDigest(key, p[pos:pos+32], -1)
	return
}

func complexHandshakeFindDigest(p []byte, key []byte, base int) int {
	gap := complexHandshakeCalcDigestPos(p, base)
	digest := complexHandshakeMakeDigest(key, p, gap)
	if !bytes.Equal(p[gap:gap+32], digest) {
		return -1
	}

	return gap
}

func complexHandshakeMakeDigest(key []byte, src []byte, gap int) (dst []byte) {
	h := hmac.New(sha256.New, key)
	if gap <= 0 {
		_, _ = h.Write(src)
	} else {
		_, _ = h.Write(src[:gap])
		_, _ = h.Write(src[gap+32:])
	}
	return h.Sum(nil)
}

func complexHandshakeCalcDigestPos(p []byte, base int) (pos int) {
	for i := 0; i < 4; i++ {
		pos += int(p[base+i])
	}
	pos = (pos % 728) + base + 4
	return
}

func complexHandshakeCreateS0S1(p []byte, time uint32, ver uint32, key []byte) {
	p1 := p[1:]
	rand.Read(p1[8:])

	common.UintAsBytes(time, p1[0:4], true)
	common.UintAsBytes(ver, p1[4:8], true)

	gap := complexHandshakeCalcDigestPos(p1, 8)
	digest := complexHandshakeMakeDigest(key, p1, gap)
	copy(p1[gap:], digest)
}

func complexHandshakeCreateS2(p []byte, key []byte) {
	rand.Read(p)
	gap := len(p) - 32
	digest := complexHandshakeMakeDigest(key, p, gap)
	copy(p[gap:], digest)
}

var (
	hsClientFullKey = []byte{
		'G', 'e', 'n', 'u', 'i', 'n', 'e', ' ', 'A', 'd', 'o', 'b', 'e', ' ',
		'F', 'l', 'a', 's', 'h', ' ', 'P', 'l', 'a', 'y', 'e', 'r', ' ',
		'0', '0', '1',
		0xF0, 0xEE, 0xC2, 0x4A, 0x80, 0x68, 0xBE, 0xE8, 0x2E, 0x00, 0xD0, 0xD1,
		0x02, 0x9E, 0x7E, 0x57, 0x6E, 0xEC, 0x5D, 0x2D, 0x29, 0x80, 0x6F, 0xAB,
		0x93, 0xB8, 0xE6, 0x36, 0xCF, 0xEB, 0x31, 0xAE,
	}
	hsServerFullKey = []byte{
		'G', 'e', 'n', 'u', 'i', 'n', 'e', ' ', 'A', 'd', 'o', 'b', 'e', ' ',
		'F', 'l', 'a', 's', 'h', ' ', 'M', 'e', 'd', 'i', 'a', ' ',
		'S', 'e', 'r', 'v', 'e', 'r', ' ',
		'0', '0', '1',
		0xF0, 0xEE, 0xC2, 0x4A, 0x80, 0x68, 0xBE, 0xE8, 0x2E, 0x00, 0xD0, 0xD1,
		0x02, 0x9E, 0x7E, 0x57, 0x6E, 0xEC, 0x5D, 0x2D, 0x29, 0x80, 0x6F, 0xAB,
		0x93, 0xB8, 0xE6, 0x36, 0xCF, 0xEB, 0x31, 0xAE,
	}
	hsClientPartialKey = hsClientFullKey[:30]
	hsServerPartialKey = hsServerFullKey[:36]
)
