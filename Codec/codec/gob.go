/*
 * @Author: zzzzztw
 * @Date: 2023-04-27 21:24:31
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-27 23:39:10
 * @FilePath: /zhang/TinyRpcByGo/Codec/codec/gob.go
 */
package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser // 由构造函数传入，通常是tcp或socket建立连接时得到的实例
	buf  *bufio.Writer      // 带缓冲的buf
	dec  *gob.Decoder       // 解码
	enc  *gob.Encoder       // 编码
}

var _ CodeC = (*GobCodec)(nil) // 验证是否重写了所有函数

func NewGobCodec(conn io.ReadWriteCloser) CodeC { //

	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}

}

func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {

	defer func() {
		_ = c.buf.Flush() // 将缓存没发送的发送了
		if err != nil {   // 关闭
			_ = c.Close()
		}
	}()

	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header", err)
		return err
	}

	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding Body", err)
		return err
	}

	return nil

}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
