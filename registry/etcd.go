/*
 * @Author: zzzzztw
 * @Date: 2023-06-02 17:21:38
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-06-05 17:33:12
 * @FilePath: /TinyRpcByGo/registry/etcd.go
 */
package registry

import (
	"context"
	"io"
	"log"
	"time"
	"tinyrpc/config"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdClient struct {
	client  *clientv3.Client
	timeout time.Duration
}

func NewEtcdClient(addr []string, timeout time.Duration) *EtcdClient {
	if timeout == 0 {
		timeout = defaultTimeout - time.Duration(1)*time.Minute
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   addr,
		DialTimeout: timeout,
	})

	if err != nil {
		log.Printf("rpc etcd: cannot connect to %s: err: %s", addr, err)
		return nil
	}
	return &EtcdClient{client: client, timeout: timeout}
}

func (e *EtcdClient) PutServer(server string) error {
	return e.Put(config.EtcdProviderPath+"/"+server, server)
}

//用于创建租约，
func (e *EtcdClient) Put(key, value string) error {

	//获取租约对象
	lease := clientv3.NewLease(e.client)
	//创建超时租约

	leaseGrantResponse, err := lease.Grant(context.Background(), int64(e.timeout/time.Second))

	if err != nil {
		return err
	}

	//将租约绑定到kv对象中去
	_, err = e.client.Put(context.TODO(), key, value, clientv3.WithLease(leaseGrantResponse.ID))
	if err != nil {
		return err
	}

	//利用心跳给key 续租
	keepAlive, err := lease.KeepAlive(context.TODO(), leaseGrantResponse.ID)

	// 消耗续约服务端返回的消息
	go leaseKeepAlive(keepAlive)
	return nil
}

//消耗服务端传来续约的消息

func leaseKeepAlive(resp <-chan *clientv3.LeaseKeepAliveResponse) {
	// 不断地取出续租成功的消息，避免塞满，一般是 1次/秒
	for true {
		select {
		case ret := <-resp:
			if ret == nil {
				return
			}
		}
	}
}

var _ io.Closer = (*EtcdClient)(nil)

func (e *EtcdClient) Close() error {
	e.client.Close()
	return nil
}
