/*
 * @Author: zzzzztw
 * @Date: 2023-06-04 16:09:22
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-06-05 17:40:18
 * @FilePath: /TinyRpcByGo/xclient/discovery_etcd.go
 */
package xclient

import (
	"context"
	"log"
	"time"
	"tinyrpc/config"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdRegistryDiscory struct {
	*TinyRegistryDiscory                  // 继承
	client               *clientv3.Client // 我们自己的注册中心是个http服务，而etcd就唯一一个连接
	timeout              time.Duration    // 服务列表的过期时间
	lastUpdate           time.Time        // 上一次从注册中心拉取服务的时间
	cancelWatch          func()           // 取消监听协程
}

//new一个构造函数

func NewEtcdRegistryDiscory(addr string, timeout time.Duration) *EtcdRegistryDiscory {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{addr},
		DialTimeout: timeout,
	})

	if err != nil {
		log.Println("rpc discovery_etcd cannot connect: %s:%s", addr, err.Error())
		return nil
	}

	etcd := &EtcdRegistryDiscory{
		TinyRegistryDiscory: NewTinyRegistryDiscovery(addr, timeout),
		client:              client,
		timeout:             timeout,
	}
	ctx, cancelfunc := context.WithCancel(context.Background())
	etcd.cancelWatch = cancelfunc
	go etcd.watchProviders(ctx)
	return etcd

}

func (e *EtcdRegistryDiscory) watchProviders(ctx context.Context) {
	watchChan := clientv3.NewWatcher(e.client).Watch(context.TODO(), config.EtcdProviderPath, clientv3.WithPrefix())

	select {
	case <-watchChan:
		for _ = range watchChan {
			// 这里可以做得更精细，因为 etcd 会给出变化的 key，我们权且简单处理
			// 结点产生了变化，就从服务器拉取
		}
		e.refreshFromEtcd()
	case <-ctx.Done():
	}
}

//重写服务发现的接口方法

func (e *EtcdRegistryDiscory) Refresh() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.lastUpdate.Add(e.timeout).After(time.Now()) {
		return nil
	}

	log.Println("rpc&&etcd discorvery: refresh providers from regitry")
	return e.refreshFromEtcd()
}

func (e *EtcdRegistryDiscory) refreshFromEtcd() error {
	resp, err := e.client.Get(context.Background(), config.EtcdProviderPath, clientv3.WithPrefix())

	if err != nil {
		log.Println("rpc&&etcd discovery: refresh err:", err)
		return err
	}

	e.servers = make([]string, 0, resp.Count)

	for i, _ := range resp.Kvs {
		e.servers = append(e.servers, string(resp.Kvs[i].Value))
	}
	e.lastUpdate = time.Now()
	return nil
}

func (e *EtcdRegistryDiscory) Get(mode SelectMode) (string, error) {
	if err := e.Refresh(); err != nil {
		return "", err
	}
	return e.MultiServersDiscovery.Get(mode)
}

func (e *EtcdRegistryDiscory) GetAll() ([]string, error) {
	if err := e.Refresh(); err != nil {
		return nil, err
	}
	return e.MultiServersDiscovery.GetAll()
}

func (e *EtcdRegistryDiscory) Close() error {
	e.cancelWatch()  // 先取消监听
	e.client.Close() // 然后关闭
	return nil
}
