/*
 * @Author: zzzzztw
 * @Date: 2023-05-01 15:51:20
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-01 18:39:14
 * @FilePath: /TidyRpcByGo/xclient/discovery.go
 */
package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

//假设有多个服务实例，每个实例提供相同的功能
//为了提高整个系统的吞吐量，每个实例部署在不同的机器上。
//客户端可以选择任意一个实例进行调用，获取想要的结果。

type SelectMode int

const (
	RandomSelect SelectMode = iota
	RoundRobinSelect
)

// 发现服务的接口
//1. Refresh() 从注册中心更新到服务列表
//2. Update(servers interface{}) 手动更新某个服务到服务列表
//3. Get(mode SelectMode) 根据负载均衡策略，选择一个服务实例
//4. GetAll() ([]string, error) 返回所有服务实例
type Discoery interface {
	Refresh() error
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}

// 用于发现服务的结构体
// 1. r 产生的随机数
// 2. index记录RoundRobin算法已经轮询到的位置，避免每次从0开始，初始化设定一个值
type MultiServersDiscovery struct {
	r       *rand.Rand
	mu      sync.RWMutex
	servers []string
	index   int
}

var _ Discoery = (*MultiServersDiscovery)(nil)

func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery {
	d := &MultiServersDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

func (d *MultiServersDiscovery) Refresh() error {
	return nil
}

func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := len(d.servers)

	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}

	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc dicovery: not support this select mode")
	}
}

func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	servers := make([]string, len(d.servers))

	copy(servers, d.servers)
	return servers, nil
}
