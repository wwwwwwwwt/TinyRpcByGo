/*
 * @Author: zzzzztw
 * @Date: 2023-04-27 23:29:40
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-06-14 21:27:47
 * @FilePath: /TinyRpcByGo/server.go
 */
package tinyrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
	"tinyrpc/codec"
)

const MagicNumber = 0x3bef5c

/*
	定义协商消息的编解码方式
*/
type Option struct {
	MagicNumber    int        //用于验证这是tinyrpc的请求头
	CodecType      codec.Type //指定选择的解码编码格式，gob or json
	ConnectTimeout time.Duration
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

/*
| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|
在一次连接中，Option固定在报文最前面header和body可能会有多个
| Option | Header1 | Body1 | Header2 | Body2 | ...
*/

type Server struct {
	serviceMap sync.Map
}

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

	// 给客户端一个响应，说明此次 Option 是 ok 的
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc server: option error: ", err)
		return
	}
	server.serverCodec(f(conn), opt.HandleTimeout)
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
	mtype        *methodType
	svc          *service
}

/*
1. 在一次连接中允许多个请求，即多个请求头和请求体

2. 进行消息体的处理，由于是并发的，为了保证消息发送的有序，所以需要加锁一条一条发送

3. 方法：
	读取请求readRequest
	处理请求handleRequest
	发送sendResponse
*/
func (server *Server) serverCodec(cc codec.Codec, timeout time.Duration) {

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
		go server.handleRequest(cc, req, sendLock, wgcv, timeout)
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

	// 1. 目前还不知道args的类型，第一个版本先只支持string(fix)
	// 2. 通过反射拿到客户端发来请求的service.method ，method包括方法名，入参变量结构体， 返回结果结构体
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)

	if err != nil {
		return req, nil
	}

	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// 确保argvi 是指针，读请求体需要指针才能修改argv的内容

	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}

	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
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

func (server *Server) handleRequest(cc codec.Codec, req *request, sendLock *sync.Mutex, wgcv *sync.WaitGroup, timeout time.Duration) {
	defer wgcv.Done()

	/*err := req.svc.call(req.mtype, req.argv, req.replyv)
	if err != nil {
		req.h.Error = err.Error()
		server.sendResponse(cc, req.h, invalidRequest, sendLock)
		return
	}
	//log.Println(req.h, req.argv.Elem())

	//req.replyv = reflect.ValueOf(fmt.Sprintf("tinyrpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sendLock)*/

	call := make(chan struct{})
	send := make(chan struct{})

	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		select {
		case call <- struct{}{}:
		default:
			return
		}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sendLock)
			send <- struct{}{}
			return
		}
		server.sendResponse(cc, req.h, req.replyv.Interface(), sendLock)
		send <- struct{}{}
	}()

	if timeout == 0 {
		<-call
		<-send
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sendLock)
	case <-call:
		<-send
	}
}

//-------------------------------------------------------------------------------------
// 具体的服务方法注册逻辑，sync.map[服务名]服务的实例
func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

func Register(rcvr interface{}) error { return DefaultServer.Register(rcvr) }

/*
ServiceMethod 的构成是 “Service.Method”，因此先将其分割成 2 部分
第一部分是 Service 的名称，第二部分即方法名。
现在 serviceMap 中找到对应的 service 实例，
再从 service 实例的 method 中，找到对应的 methodType。
*/
func (server *Server) findService(ServiceMethod string) (svc *service, mtype *methodType, err error) {

	dot := strings.LastIndex(ServiceMethod, ".")

	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + ServiceMethod)
		return
	}

	serviceName, methodName := ServiceMethod[:dot], ServiceMethod[dot+1:] //左开右闭

	svci, ok := server.serviceMap.Load(serviceName)

	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
	}

	svc = svci.(*service)

	mtype = svc.method[methodName]

	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}

	return

}

//-------------------------------------------------------------------------------------
//支持http协议部分
//客户端会向服务端发送connect请求，rpc服务器返回Http 200状态码表示连接
//接下来客户端使用创建好的连接发送rpc报文，先发送Json格式的Option协商编码格式，再发送请求报文

var _ http.Handler = (*Server)(nil)

const (
	connected        = "200 Connected to RPC"
	defaultRPCPath   = "/_tinyrpc_"
	defaultDebugPath = "/debug/tinyrpc"
)

// 实现由http连接到rpc连接的转换
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 首先判断请求方法类型
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}

	conn, _, err := w.(http.Hijacker).Hijack() // 劫持这个conn用作rpc连接

	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	// 写回 http 响应消息，格式为：响应行\n响应头（头部行）\n响应体。由于没有加响应头（头部行），所以这里末尾写了两个\n
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	server.ServerConn(conn)
}

func (server *Server) HandleHttp() {
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Println("rpc server debug path:", defaultDebugPath)
}

func HandleHttp() {
	DefaultServer.HandleHttp()
}
