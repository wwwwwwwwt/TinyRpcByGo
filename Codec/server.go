/*
 * @Author: zzzzztw
 * @Date: 2023-04-27 23:29:40
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-29 10:42:20
 * @FilePath: /TidyRpcByGo/Codec/server.go
 */
package tinyrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"tinyrpc/codec"
)

const MagicNumber = 0x3bef5c

/*
	定义协商消息的编解码方式
*/
type Option struct {
	MagicNumber int        //用于验证这是tinyrpc的请求头
	CodecType   codec.Type //指定选择的解码编码格式，gob or json
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

/*
| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|
在一次连接中，Option固定在报文最前面header和body可能会有多个
| Option | Header1 | Body1 | Header2 | Body2 | ...
*/

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

/*
建立一个默认服务器实例，方便使用
如果想启动服务，传入 listener 即可，tcp 协议和 unix 协议都支持。
lis, _ := net.Listen("tcp", ":9999")
tiny.Accept(lis)
*/
var DefaultServer = NewServer()

// 循环处理链接，对每个连接进行连接协议检查
func (server *Server) Accept(lis net.Listener) {

	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error", err)
			return
		}
		go server.ServerConn(conn)
	}

}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

/*
1. 首先通过json.NewDecoder反序列化得到Option

2. 检查协议: MagicNumber CodeType

3. 然后根据CodeType得到对应消息的编解码器，然后交给serverCode进行处理

*/
func (server *Server) ServerConn(conn io.ReadWriteCloser) {

	defer func() {
		_ = conn.Close()
	}()

	var opt Option

	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpv server: options error", err)
		return
	}

	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number: %x", opt.MagicNumber)
		return
	}

	f := codec.NewCodecFuncMap[opt.CodecType]

	if f == nil {
		log.Printf("rpc sever: invalid codec type %s", opt.CodecType)
		return
	}
	server.serverCodec(f(conn))
}

var invalidRequest = struct{}{} // 用于当发生错误解码时，发送的占位接口

/*
	保存消息属性
	消息头:header
	传给服务端；args / reply
*/
type request struct {
	h            *codec.Header
	argv, replyv reflect.Value
}

/*
1. 在一次连接中允许多个请求，即多个请求头和请求体

2. 进行消息体的处理，由于是并发的，为了保证消息发送的有序，所以需要加锁一条一条发送

3. 方法：
	读取请求readRequest
	处理请求handleRequest
	发送sendResponse
*/
func (server *Server) serverCodec(cc codec.Codec) {

	sendLock := new(sync.Mutex)
	wgcv := new(sync.WaitGroup)

	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sendLock)
			continue
		}
		wgcv.Add(1)
		go server.handleRequest(cc, req, sendLock, wgcv)
	}
	wgcv.Wait()
	_ = cc.Close()
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header

	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header err", err)
		}
		return nil, err
	}
	return &h, nil

}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {

	h, err := server.readRequestHeader(cc)

	if err != nil {
		return nil, err
	}

	req := &request{h: h}

	// 目前还不知道args的类型，第一个版本先只支持string

	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		fmt.Println("rpc server :read argv err", err)
	}

	return req, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sendLock *sync.Mutex) {
	sendLock.Lock()
	defer sendLock.Unlock()

	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sendLock *sync.Mutex, wgcv *sync.WaitGroup) {
	defer wgcv.Done()

	log.Println(req.h, req.argv.Elem())

	req.replyv = reflect.ValueOf(fmt.Sprintf("tinyrpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sendLock)
}
