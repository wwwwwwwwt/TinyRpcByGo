/*
 * @Author: zzzzztw
 * @Date: 2023-04-27 21:02:51
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-28 17:13:38
 * @FilePath: /TidyRpcByGo/Codec/codec/codec.go
 */
package codec

import "io"

/*
典型的rpc调用  err = client.Call("Arith.Multiply", args, &reply)

客户端发送的请求包括服务名Arith， 方法名 Multiply， 参数args

服务端相应的结果包括 error 和reply两个

这里将响应参数args和reply 抽象为body 服务名方法名和错误信息抽象为header

*/
type Header struct {
	ServiceMethod string // 格式 "Service.Method"
	Seq           uint64 // 请求序号，某个请求的id，用来区分不同的请求
	Error         string // 错误号， 客户端置为空，服务端若发生错误将错误放进去
}

type Codec interface { // 抽象出对消息体编解码的接口Codec， 目的是实现不同的CodeC实例
	io.Closer // 用于关闭连接的接口，需要实现Close()
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

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
