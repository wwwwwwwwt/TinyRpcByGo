/*
 * @Author: zzzzztw
 * @Date: 2023-04-27 21:02:51
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-27 21:28:17
 * @FilePath: /zhang/TidyRpcByGo/codec/codec.go
 */
package codec

import "io"

type Header struct {
	ServiceMethod string // 格式 "Service.Method"
	Seq           uint64 // 请求序号，某个请求的id，用来区分不同的请求
	Error         string // 错误号
}

type CodeC interface { // 抽象出对消息体编解码的接口Codec， 目的是实现不同的CodeC实例
	io.Closer // 用于关闭连接的接口，需要实现Close()
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodecFunc func(io.ReadWriteCloser) CodeC

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // todo
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
