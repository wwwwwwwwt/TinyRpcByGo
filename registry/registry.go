/*
 * @Author: zzzzztw
 * @Date: 2023-05-01 23:39:37
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-02 03:14:42
 * @FilePath: /TinyRpcByGo/registry/registry.go
 */
package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type TinyRegistry struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*ServerItem
}

type ServerItem struct {
	Addr  string
	start time.Time
}

const (
	defaultPath    = "/_tinyrpc_/registry"
	defaultTimeout = time.Minute * 5
)

func NewRegistry(timeout time.Duration) *TinyRegistry {
	return &TinyRegistry{
		servers: make(map[string]*ServerItem),
		timeout: timeout,
	}
}

var DefaultRegitsry = NewRegistry(defaultTimeout)

//-------------------------------------------------------------------------------------
// 实现添加服务实例和返回服务列表的方法
// putServer 添加服务实例，如果服务已经存在则更新start时间
// aliveServers 返回可用的服务列表，如果存在超时的服务，则删除

func (r *TinyRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s := r.servers[addr]

	if s == nil {
		r.servers[addr] = &ServerItem{Addr: addr, start: time.Now()}
	} else {
		s.start = time.Now()
	}
}

func (r *TinyRegistry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var alive []string

	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}

	sort.Strings(alive)

	return alive
}

//-------------------------------------------------------------------------------------
// 注册中心采用HTTP协议，信息都保存在HTTP Header中，继承http.handler,需要重写ServeHTTP方法
// Get  返回所有的可用服务列表，通过自定义字段X-tinyrpc-Servers承载
// Post 添加服务实例或发送心跳，通过自定义字段X-tinurpc-Server承载
var _ http.Handler = (*TinyRegistry)(nil)

func (r *TinyRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		// 将所有可用服务写在响应头上， 然后根据","分割服务地址
		w.Header().Set("X-tinyrpc-Servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		addr := req.Header.Get("X-tinyrpc-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *TinyRegistry) HandleHTTP(regitsryPath string) {
	http.Handle(regitsryPath, r)
	log.Println("rpc registry path:", regitsryPath)
}

func HandleHTTP() {
	DefaultRegitsry.HandleHTTP(defaultPath)
}

//-------------------------------------------------------------------------------------
// 实现心跳机制

func Heartbeat(registry string, addr string, duration time.Duration) {
	//发送心跳时间比超时时间少一分钟
	if duration == 0 {
		duration = defaultTimeout - time.Duration(1)*time.Minute
	}

	var err error

	err = sendHeartbeat(registry, addr)

	go func() {
		t := time.NewTicker(duration)
		for err == nil {
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

// 与定时器channel配合定时使用POST请求发送心跳包
func sendHeartbeat(registry string, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)

	httpClient := &http.Client{}

	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-tinyrpc-Server", addr)
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}
