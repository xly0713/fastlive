package common

import "io"

// uint32 ==> []byte
func UintAsBytes(val uint32, buffer []byte, bigEndian bool) {
	n := len(buffer)
	for i := 0; i < n; i++ {
		if bigEndian {
			v := val >> ((n - i - 1) << 3)
			buffer[i] = byte(v) & 0xff
		} else {
			buffer[i] = byte(val) & 0xff
			val = val >> 8
		}
	}
}

// bytes ==> uint32
func BytesAsUint32(buffer []byte, bigEndian bool) uint32 {
	ret := uint32(0)

	n := len(buffer)
	for i := 0; i < n; i++ {
		if bigEndian {
			ret = ret<<8 + uint32(buffer[i])
		} else {
			ret += uint32(buffer[i]) << uint32(i*8)
		}
	}

	return ret
}

// read some bytes, then treats as uint32
func ReadBytesAsUint32(r io.Reader, buffer []byte, bigEndian bool) (uint32, error) {
	if _, err := r.Read(buffer); err != nil {
		return 0, err
	}

	return BytesAsUint32(buffer, bigEndian), nil
}

var (
	FmtToMessageHdrSize = [4]int{11, 7, 3, 0}
)
