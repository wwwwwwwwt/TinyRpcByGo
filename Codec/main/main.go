/*
 * @Author: zzzzztw
 * @Date: 2023-04-28 12:25:51
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-29 10:44:22
 * @FilePath: /TidyRpcByGo/Codec/main/main.go
 */
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
	"tinyrpc"
	"tinyrpc/codec"
)

func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	tinyrpc.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple tinyrpc client
	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	//time.Sleep(time.Second)
	// send options
	_ = json.NewEncoder(conn).Encode(tinyrpc.DefaultOption)
	time.Sleep(time.Second)
	cc := codec.NewGobCodec(conn)
	// send request & receive response
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("tinyrpc req %d", h.Seq))

		_ = cc.ReadHeader(h)

		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
