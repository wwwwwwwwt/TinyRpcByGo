/*
 * @Author: zzzzztw
 * @Date: 2023-04-28 12:25:51
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-01 15:42:47
 * @FilePath: /TidyRpcByGo/main/main.go
 */
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
	"tinyrpc"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	//time.Sleep(2 * time.Second)
	*reply = args.Num1 + args.Num2
	return nil
}

/*func startServer(addr chan string) {
	var foo Foo
	if err := tinyrpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
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
	client, _ := tinyrpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			ctx, _ := context.WithTimeout(context.Background(), time.Second)
			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}*/

func startServer(addrCh chan string) {
	var foo Foo
	l, _ := net.Listen("tcp", ":9999")
	_ = tinyrpc.Register(&foo)
	tinyrpc.HandleHttp()
	addrCh <- l.Addr().String()
	_ = http.Serve(l, nil)
}
func call(addrCh chan string) {
	client, _ := tinyrpc.DialHTTP("tcp", <-addrCh)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}

func main() {
	log.SetFlags(0)
	ch := make(chan string)
	go call(ch)
	startServer(ch)
}
