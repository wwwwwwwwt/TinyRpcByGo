/*
 * @Author: zzzzztw
 * @Date: 2023-05-02 03:05:01
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-02 03:07:20
 * @FilePath: /TinyRpcByGo/xclient/discovery_rpc.go
 */
package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

//-------------------------------------------------------------------------------------
// 实现注册中心发现服务
// 嵌套了 MultiServersDiscovery，很多方法可以复用。
// registry 注册中心的地址
// timeout 服务列表过期的时间
// 最后从注册中心更新服务列表的时间
// 默认10s过期，即10s过后，需要从注册中心更新新的列表
type TinyRegistryDiscory struct {
	// 继承发现服务的结构体，包括负载均衡算法
	*MultiServersDiscovery
	registry   string
	timeout    time.Duration
	lastUpdate time.Time
}

const defaultUpdateTimeout = time.Second * 10

func NewTinyRegistryDiscovery(registerAddr string, timeout time.Duration) *TinyRegistryDiscory {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}

	d := &TinyRegistryDiscory{
		MultiServersDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:              registerAddr,
		timeout:               timeout,
	}
	return d
}

func (d *TinyRegistryDiscory) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

func (d *TinyRegistryDiscory) Refresh() error {

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}

	log.Println("rpc register: refresh servers from registry", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Panicln("rpc registry refresh err:", err)
		return err
	}

	servers := strings.Split(resp.Header.Get("X-tinyrpc-Servers"), ",")

	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

func (d *TinyRegistryDiscory) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(mode)
}

func (d *TinyRegistryDiscory) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}

	return d.MultiServersDiscovery.GetAll()
}
