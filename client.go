/*
 * @Author: zzzzztw
 * @Date: 2023-04-29 11:25:12
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-01 17:49:11
 * @FilePath: /TidyRpcByGo/client.go
 */
package tinyrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"tinyrpc/codec"
)

type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call
}

func (call *Call) done() {
	call.Done <- call // 函数调用结束后，通过done()通知调用方
}

type Client struct {
	cc       codec.Codec      // 解码器
	opt      *Option          // 验证报文Option
	sendLock sync.Mutex       // 涉及发送消息的互斥锁
	mu       sync.Mutex       // 其他操作的互斥锁
	header   codec.Header     // 每个报文请求头，只有在请求发送时才需要，请求发送时互斥的，所以每个客户端都需要一个
	seq      uint64           // 发送的请求编号，每个请求都有一个
	pending  map[uint64]*Call // 存储未处理完的请求，key 编号seq，val是Call实例,类似消息队列
	closing  bool             // 手动关闭
	shutdown bool             // 由于错误的关闭
}

func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shutdwon")

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close() // 关闭解码器连接
}

//---------------------------------------------------------------------
//客户端实现操作call方法

// 1.将call添加进client.pending中，并更新client.seq
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// 2.根据seq从client中移除对应的call，并返回call
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

// 3. 服务端或客户端发生错误时调用，并将shutdown设为true，将错误信息通知所有pending中的call
func (client *Client) terminalCalls(err error) {
	client.sendLock.Lock()
	defer client.sendLock.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

//---------------------------------------------------------------------

// 客户端的接收响应和发送请求
// 1. 接收响应，call有三种情况
// 1.1 call不存在，原因可能是请求没发送完整，或因为其他原因被取消，服务端仍处理了
// 1.2 call存在，但服务端处理错了，即header h.Error 不为空
// 1.3 call存在且被正确处理，那么需要从body读出reply的结果

func (client *Client) receive() {
	var err error

	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		call := client.removeCall(h.Seq)

		switch {
		case call == nil:
			// call不存在
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// 存在但服务端处理报错了
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			//call存在且被正确处理，就解析body拿出结果
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body" + err.Error())
			}
			call.done()
		}
	}

	// 发生错误了直接终止
	client.terminalCalls(err)
}

//---------------------------------------------------------------------
// 创建实例，首先完成协议的交换，把Option发送给客户端，协商好消息的编码解码方式后
// 创建一个字协程receive()接收响应

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		return nil, err
	}

	// send opt with server
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error", err)
		_ = conn.Close()
		return nil, err
	}
	//接收一个服务端发来的响应，说明解析完了opt，然后再发送请求消息，防止粘包
	if err := json.NewDecoder(conn).Decode(opt); err != nil {
		log.Println("rpc client: option err: ", err)
		_ = conn.Close()
		return nil, err
	}

	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		cc:      cc,
		opt:     opt,
		seq:     1,
		pending: make(map[uint64]*Call),
	}
	go client.receive() // 每开一个实例就起一个receive协程去接收响应
	return client
}

// 封装一下传进来的opt可选项参数
func parseOption(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || len(opts) == 1 {
		return DefaultOption, nil
	}
	if len(opts) > 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]

	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

type clientResult struct {
	client *Client
	err    error
}

//实现客户端连接超时自动关闭功能
func dialTimeout(f newClientFunc, network string, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOption(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)

	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	ch := make(chan clientResult)

	go func() {
		client, err := f(conn, opt)
		//ch <- clientResult{client: client, err: err}// 若主线程超时结束了，这个ch中的数据没被拿走将被阻塞，造成内存泄露
		// 修改为能放进管道就放，不能就走default
		select {
		case ch <- clientResult{client: client, err: err}:
		default:
		}
	}()

	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}

	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}

}

// 实现dial函数，便于用户传入服务端地址，直接创建Client实例
func Dial(network string, address string, opts ...*Option) (client *Client, err error) {
	/*
		// 通过封装拿到opt
		opt, err := parseOption(opts...)
		if err != nil {
			return nil, err
		}
		conn, err := net.Dial(network, address) // 通过"tcp / udp", 地址进行连接
		if err != nil {
			return nil, err
		}
		defer func() {
			if client == nil {
				_ = conn.Close()
			}
		}()

		return NewClient(conn, opt) //返回创建一个client实例*/

	return dialTimeout(NewClient, network, address, opts...)
}

//---------------------------------------------------------------------
// 客户端的发送请求send()方法
func (client *Client) send(call *Call) {
	client.sendLock.Lock()
	defer client.sendLock.Unlock()

	// 注册call
	seq, err := client.registerCall(call)

	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// 准备请求头
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Error = ""
	client.header.Seq = call.Seq

	// 编码并发送请求
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)

		if call != nil {
			call.Error = err
			call.done()
		}

	}
}

//异步接口，返回Call的实例
func (client *Client) Go(serviceMethod string, args interface{}, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}

	client.send(call)
	return call
}

//同步接口，receive() 后说明调用结束，调用done(), 此时会将调用好的call放进信道Done
func (client *Client) Call(ctx context.Context, serviceMethod string, args interface{}, reply interface{}) error {
	//call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case ca := <-call.Done:
		return ca.Error
	}

	//用户可以使用创建有超时检测功能的context对象来控制
	//ctx, _ := context.WithTimeout(context.Background(), time.Second)
	//err := client.Call(ctx, "Foo.Sum", &Args{1, 2}, &reply)
}

//-------------------------------------------------------------------------------------
//客户端支持http协议

func NewHTTPclient(conn net.Conn, opt *Option) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})

	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}

	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}

	return nil, err
}

func DialHTTP(network string, address string, opts ...*Option) (*Client, error) {
	//使用HTTP方法
	return dialTimeout(NewHTTPclient, network, address, opts...)
}

//http和rpc统一的api
func XDial(rpcAddr string, opts ...*Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")

	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}

	protocol, addr := parts[0], parts[1]

	switch protocol {
	case "http":
		//处理http协议连接， 底层通信还是tcp
		return DialHTTP("tcp", addr, opts...)
	default:
		// tcp, unix
		return Dial(protocol, addr, opts...)
	}
}
