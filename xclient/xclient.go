/*
 * @Author: zzzzztw
 * @Date: 2023-05-01 17:30:53
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-02 03:05:18
 * @FilePath: /TinyRpcByGo/xclient/xclient.go
 */
package xclient

import (
	"context"
	"io"
	"reflect"
	"sync"
	. "tinyrpc"
)

type XClient struct {
	d       Discoery
	mode    SelectMode
	opt     *Option
	mu      sync.Mutex
	clients map[string]*Client
}

var _ io.Closer = (*XClient)(nil)

func NewXClient(dis Discoery, mde SelectMode, op *Option) *XClient {
	return &XClient{d: dis, mode: mde, opt: op, clients: make(map[string]*Client)}
}

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	for key, val := range xc.clients {

		_ = val.Close()
		delete(xc.clients, key)
	}

	return nil
}

//-------------------------------------------------------------------------------------
// 负载均衡实现

// 1. 检查 xc.clients 是否有对应Address的缓存的 Client，如果有，检查是否是可用状态，如果是则返回缓存的 Client，如果不可用，则从缓存中删除。
// 2. 如果步骤 1) 没有返回缓存的 Client，则说明需要创建新的 Client，缓存并返回。
func (xc *XClient) dial(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	client, ok := xc.clients[rpcAddr]

	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}

	if client == nil {
		var err error
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
	}
	return client, nil
}

// 通过服务器地址拿到与该Server对应的client， 底层调用Client
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args interface{}, replyv interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, args, replyv)
}

// 封装call，调用对应的负载均衡策略，并对外暴露
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args interface{}, replyv interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)

	if err != nil {
		return err
	}

	return xc.call(rpcAddr, ctx, serviceMethod, args, replyv)
}

// 向所有服务端广播注册这个服务
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args interface{}, replyv interface{}) error {

	servers, err := xc.d.GetAll()

	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var e error

	replyDone := replyv == nil
	contx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}
			if replyv != nil {
				//clonedReply = reflect.New(reflect.ValueOf(replyv).Elem().Type()).Interface() // 深拷贝一个指针， 根据valueof的type new一个新的val
				//                                Value     具体的Value  具体Value的Type
				//                                 |              |      |
				clonedReply = reflect.New(reflect.ValueOf(replyv).Elem().Type()).Interface()
				//                    |                                            |
				//          根据具体Value的Type new一个新Value                   返回新Value内部的值

			}
			err := xc.call(rpcAddr, contx, serviceMethod, args, clonedReply)
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel()
			}

			if err == nil && !replyDone {
				reflect.ValueOf(replyv).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()

		}(rpcAddr)
	}

	wg.Wait()

	return e
}
