/*
 * @Author: zzzzztw
 * @Date: 2023-05-01 23:39:37
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-01 23:49:17
 * @FilePath: /TinyRpcByGo/registry/registry.go
 */
package registry

import (
	"sync"
	"time"
)

type TinyRegistry struct {
	timeout time.Duration
	my      sync.Mutex
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
