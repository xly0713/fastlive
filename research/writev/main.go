package main

import (
	"fmt"
	"net"
	"sync"
)

type conn struct {
	rwc      net.Conn
	wbuf     net.Buffers
	bytepool sync.Pool
}

func (c *conn) write(p []byte) (int64, error) {
	c.wbuf = append(c.wbuf, p)
	return 0, nil
}

func (c *conn) flush() (int64, error) {
	if len(c.wbuf) <= 0 {
		return 0, nil
	}

	for _, bs := range c.wbuf {
		defer c.bytepool.Put(bs)
	}

	if nw, err := c.wbuf.WriteTo(c.rwc); err != nil {
		return 0, err
	} else {
		return nw, nil
	}
}

func main() {
	tcpConn, err := net.Dial("tcp", "127.0.0.1:6666")
	if err != nil {
		fmt.Println(err)
		return
	}

	c := &conn{
		rwc:  tcpConn,
		wbuf: make(net.Buffers, 0, 10),
		bytepool: sync.Pool{
			New: func() interface{} {
				fmt.Println("new byte slice")
				return make([]byte, 0, 10)
			},
		},
	}

	for i := 0; i < 3; i++ {
		str := fmt.Sprintf("num: %d\n", i)

		bs := c.bytepool.Get().([]byte)
		bs = append(bs, []byte(str)...)

		if nw, err := c.write(bs); err != nil {
			fmt.Printf("err: %v\n", err)
		} else if nw != 0 {
			fmt.Printf("has written: %d\n", nw)
		}
	}

	if nw, err := c.flush(); err != nil {
		fmt.Printf("err: %v\n", err)
		return
	} else {
		fmt.Printf("has flushed: %d\n", nw)
	}
	fmt.Printf("buf len: %d, cap: %d\n", len(c.wbuf), cap(c.wbuf))

	for i := 0; i < 4; i++ {
		str := fmt.Sprintf("num: %d\n", i)

		bs := c.bytepool.Get().([]byte)
		bs = append(bs, []byte(str)...)

		if nw, err := c.write(bs); err != nil {
			fmt.Printf("err: %v\n", err)
		} else if nw != 0 {
			fmt.Printf("has written: %d\n", nw)
		}
	}

	if nw, err := c.flush(); err != nil {
		fmt.Printf("err: %v\n", err)
	} else {
		fmt.Printf("has flushed: %d\n", nw)
	}

	fmt.Printf("buf len: %d, cap: %d\n", len(c.wbuf), cap(c.wbuf))

	select {}
}
