<!--
 * @Author: zzzzztw
 * @Date: 2023-04-27 18:33:45
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-05-02 03:16:14
 * @FilePath: /TinyRpcByGo/README.md
-->
# 基于Go的简易rpc框架🚀

仿照Go 语言官方的标准库 net/rpc，进行开发，并在此基础上，新增了协议交换、注册中心、服务发现、负载均衡、超时处理等特性。

### 主要特点：
- 🔨: 编解码部分基于 Json、Gob 格式
- 🎯: 负载均衡采用了客户端负载均衡策略，实现了随机、轮询两种算法(finish)
- ⏰: 使用 time.After 和 select-chan 机制为客户端连接、服务端处理添加了超时处理机制 (finish)
- ☁: 实现了简易的注册中心和心跳机制，同时支持了Etcd(TODO)


### 客户端

#### 客户端的创建

1. 请求进来，到达负载均衡客户端；
2. 负载均衡客户端调用服务发现模块获取一个服务的[ip:port]，服务发现模块通过自己的负载均衡策略给负载均衡客户端返回一个[ip:port]，负载均衡客户端通过这个[ip:port]从自身维护的map中取出与之对应的通信客户端。
3. 判断通信客户端是否关闭或者为空，如果是，则移除并重新创建通信客户端。创建通信客户端的方法是支持多协议的，通过 ip 上的协议名称，传入不同的客户端创建策略进行创建。具体来说，创建一个通信客户端分为以下几步：
   1. 调用库函数 net.DialTimeout 得到一个conn
   2. 启用一个新协程，使用conn去调用客户端创建策略方法，并给这个协程设置超时（select chan + time after）
   3. 客户端创建策略方法会根据自身的协议去创建一个客户端，如果再超时时间内会把这个客户端通过channel返回给父协程。客户端的创建策略目前有两种：
      - HTTP 客户端策略：会与服务端进行一次http的 CONNECT 通信，使得这个 conn 能够被服务端劫持，接着再去创建一个 rpc 客户端。从而使得这个连接既可以传输 HTTP 请求，又可以传输我们自定义的 rpc 消息格式；
      - rpc 客户端策略：使用Json格式的数据与服务端进行两次握手，从而与服务器协商用户自定好的编解码器，并且启动一个接收协程。

4. 创建好的通信客户端会被返回给负载均衡客户端，用对应[ip:port]存起来，以便复用。然后使用这个通信客户端进行rpc调用

#### 客户端调用

1. 通信客户端会将这次的调用请求封装成 call，call里面包含了唯一标识（seq）、请求的方法、参数、返回值以及一个chan，并且使用唯一标识，在自己的map中维护这个call
2. 之后，会将call中的具体的参数封装成协议要求的格式（header + body：header 包括 seq、服务方法名、错误；body为参数），通过编码器写给服务端。这个过程通过 call 中的 chan 实现了异步调用，发送完消息之后，直接返回这个  call。
3. 一次调用的过程：
   * 注册：将结构体实例通过Register传进服务端，开一个新服务实例，这个实例的map中保存着这个结构体（服务）的所有方法method，再将结构体名字string和对应着的服务实例注册到一个服务端map中。
   * 服务端调用方法：将结构体实例传进服务端，服务端通过解码，得到消息头和消息体，消息头包括服务的名字（string，形如T.Method）和序号等，消息体包括参数Call结构体，内部包括序号，服务名，入参和返回类型（interface{}）等。通过对请求头的解析得到服务的名字，再通过.的位置分隔出服务实例的名字string和方法string， 从服务端的map中取出对应名字的实例，和从实例中取出对应的方法method。 然后对请求体解析得到入参的结构体，使用反射的Call调用方法得到replyv。最后通过gob编码encode 进buf 再 flush 发送回客户端
   * 客户端接收消息：如下

#### 客户端接收结果

1. 在创建通信客户端的时候，除了与服务端协商编码，还会启动一个接收协程。
2. 接收协程死循环接收服务端的响应。
3. 接收协程接在收到服务返回的消息后，会将返回结果解析成 call，call 中的返回值字段即用来承载服务端的处理结果。收到消息后，会给这个call 中的 chan 字段写入“处理完成”的消息，即通知持有这个 call 的用户，结果已经接收到了，可以进行下一步操作了。处理完成之后，会从通信客户端中移除这个call（因为它已经完成了它的使命）
4. 在这个循环过程中，如果发现了消息解析错误或者网络io错误，会跳出这个循环，然后中断这个通信客户端中所有 call，告诉它们有错误发生（因为前面的解析或者网络错误，会导致后面的所有 call 都出现问题，那么没必要再等服务器的消息了）


---



### 服务端

#### 接收连接

1. 在主协程中死循环 Accept，每收到一个连接，会启动一个新的协程去处理这个连接
2. 一个新的连接进来，要建立可用的rpc通信，需要进行编码协商（这也是新建通信客户端时候所做的事）。那么服务端在连接处理协程中，就需要先处理这个事情，然后才能rpc通信。具体分为以下几个步骤：
   1. 解析编码协商报文
   2. 判断魔术
   3. 根据编码协商报文中的编码字段创建响应编解码器
   4. 响应客户端，通知协商成功（客户端接收到成功响应之后，才能发送请求，防止粘包）

#### 处理连接

1. 编码协商成功后，连接处理协程会进入死循环接收该连接中的报文（因为一次连接中可以有多次调用（header+body），那么需要尽力而为）
2. 因为一个连接可能有多次调用，而考虑到这些调用是可以并发进行，连接处理协程在解析一个header+body对之后，就会启动一个请求处理协程，去处理这个请求，执行相应的方法。并且如果某一次读取解析header+body出现了错误，那么就需要跳出循环，关闭这个连接，而此时前面还有header+body的调用正在请求处理协程中进行处理，所以不能立即关闭连接，那么就引入了 WaitGroup 等待所有请求处理协程处理完毕之后再关闭连接
3. 执行一个方法的时间可长可短，在请求处理协程中，也采用了（协程调用 + select chan + time after 的超时机制）
4. 因为调用完后，写给客户端的返回值也是header+body，那么就需要保证在这个连接中header+body是成对写入的，不能出现交织，所以需要用锁控制header+body写回的原子性。

### 服务


1. 因为客户端的请求是 service.method，所以我们分别定义了 service 结构体和 methodType 结构体分别用来保存“一个用来提供服务的结构体”和“其方法被调用所需要“的各项信息
2. service 对象代表着一个提供服务的对象，methodType 对象代表着这个提供服务的对象的一个方法。所以 service 以 map 持有多个 methodType。
3. 提供了将一个结构体对象转变成 service对象，其中的合规方法转变成 methodType 的函数。（合规的方法的签名只能有两个入参，前一个代表参数，后一个代表返回值，和一个错误返回值）
4. 将 service 集成进 Server，使得 Server能够注册 service，持有多个 service，并且在处理请求时，能够调用到对应 service 的 method 上
5. 通过 ```method.Func.Call([]reflect.Value{s.rcvr, argv, replyv})```调用对象的方法

---



### 注册中心

#### 简易注册中心

1. 这个注册中心维护了一个 [服务器地址 -> 服务器地址 + 该服务上一次的心跳时间] 的 map，并且通过实现 http.Handler 接口，对外提供 Http 服务，这样每个服务器可以通过 POST 请求发送心跳、服务发现模块通过 GET 请求拉取所有可用服务器的地址。
2. 注册中心在响应服务发现模块的GET请求时，会遍历一遍自己维护的服务列表，剔除掉已经超时的服务，然后通过 HTTP 自定义头，返回所有存活的服务地址。
3. 此外，还暴露了对外的 Heartbeat 函数，使得服务可以使用该函数向指定注册中心发送指定服务的心跳

#### Etcd注册中心(TODO)

### 服务发现

#### 简易服务发现模块

1. 简易服务发送模块，内部维护了从注册中心全量拉取的服务器地址
2. 并且在每次 Get 服务器地址时，会使用 Refresh 方法根据设定好的超时时间，判断是否要去注册中心全量拉取一次
3. 更新完注册中心地址后，会通过设定好的负载均衡算法，从服务器地址列表中返回一个选中的服务器地址给通信客户端

#### Etcd服务发现模块(TODO)
---

\*************************************************************************************************************\*  
以下为开发时遇到的问题:
1.  bug1:客户端一开始发送json格式Option时 

```go

/*
| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|
在一次连接中，Option固定在报文最前面header和body可能会有多个
| Option | Header1 | Body1 | Header2 | Body2 | ...
*/

客户端发送：
_ = json.NewEncoder(conn).Encode(tinyrpc.DefaultOption)
cc := codec.NewGobCodec(conn)

服务端解码

if err := json.NewDecoder(conn).Decode(&opt); err != nil {
log.Println("rpv server: options error", err)
return
}

由于没有确定边界，当缓冲区有多条消息堆积时，可能json把Header的内容读出，导致Header内容缺失，形成阻塞


```

* server端解析Option的时候可能会破坏后面RPC消息的完整性，当客户端消息发送过快服务端消息积压时（例：Option|Header|Body|Header|Body），服务端使用json解析Option，json.Decode()调用conn.read()读取数据到内部的缓冲区（例：Option|Header），此时后续的RPC消息就不完整了(Body|Header|Body)。  
目前代码中客户端简单的使用time.sleep()方式隔离协议交换阶段与RPC消息阶段，减少这种问题发生的可能。（fix bug）  
（new）通过客户端和服务端两次握手，确保服务端将客户端发来的option字段被处理完，客户端再发送| Header{ServiceMethod ...} | Body interface{} | 防止粘包

2.  Call的设计

* 一个典型的函数远程调用形式

```go

func (t *T)MethodName(argType *T1, replyType *T2)error

```
把所有需要的信息封装进Call

```go
type Call struct {
	Seq           uint64 // 消息的序号， 不断增长
	ServiceMethod string //服务端注册过的方法
	Args          interface{} // 参数
	Reply         interface{} // 返回的结果
	Error         error
	Done          chan *Call // 为了可以异步调用，定义管道，当调用结束后通知调用方， 
}
```


* 关于客户端接口Call与Go

	Call 是同步接口，具体使用再main中有所展现，会等待执行结果返回后再进行执行  
	Go 为异步接口，具体使用场景如下

```go
//--------------------------
//Call

go func(i int) {
	defer wg.Done()
	args := fmt.Sprintf("geerpc req %d", i)
	var reply string
	if err := client.Call("Foo.Sum", args, &reply); err != nil {
		log.Fatal("call Foo.Sum error:", err)
	}
	log.Println("reply:", reply)
}(i)

//--------------------------
//Go
call := client.Go( ... )
//新启动协程，异步等待
go func(call *Call) {
	select {
		<-call.Done:
			# do something
		<-otherChan:
			# do something
	}
}(call)

otherFunc() // 不阻塞，继续执行其他函数。
```


3.  通过反射实现结构体与服务的映射关系
  * RPC 框架的一个基础能力是：像调用本地程序一样调用远程服务。那如何将程序映射为服务呢？那么对 Go 来说，这个问题就变成了如何将结构体的方法映射为服务。  
  * 对 net/rpc 而言，一个函数需要能够被远程调用，需要满足如下五个条件：
  * the method’s type is exported. – 方法所属类型是导出的。
  * the method is exported. – 方式是导出的
  * the method has two arguments, both exported (or builtin) types. – 两个入参，均为导出或内置类型。
  * the method’s second argument is a pointer. – 第二个入参必须是一个指针。
  * the method has return type error. – 返回值为 error
  
```go
  func (t *T) MethodName(argType T1, replyType *T2) error
```

通过反射，可以获取某个结构体所有的方法，并且通过方法可以知道其所有的参数类型和返回值，具体示例如下：

```go
func main() {
	var wg sync.WaitGroup
	typ := reflect.TypeOf(&wg)
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		argv := make([]string, 0, method.Type.NumIn())
		returns := make([]string, 0, method.Type.NumOut())
		// j 从 1 开始，第 0 个入参是 wg 自己。
		for j := 1; j < method.Type.NumIn(); j++ {
			argv = append(argv, method.Type.In(j).Name())
		}
		for j := 0; j < method.Type.NumOut(); j++ {
			returns = append(returns, method.Type.Out(j).Name())
		}
		log.Printf("func (w *%s) %s(%s) %s",
			typ.Elem().Name(),
			method.Name,
			strings.Join(argv, ","),
			strings.Join(returns, ","))
    }
}

运行的结果是：
func (w *WaitGroup) Add(int)
func (w *WaitGroup) Done()
func (w *WaitGroup) Wait()

```

4. 设定定时器超时时，造成了管道阻塞内存泄漏

当主线程超时时，无缓冲管道内数据无法被拿走，导致阻塞内存泄漏

```go
	/*go func() {
		client, err := f(conn, opt) // 若主线程超时结束了，这个ch中的数据没被拿走将被阻塞，造成内存泄漏
		ch <- clientResult{client: client, err: err}
	}()*/

   // ch <- cs
   // 这里会有内存泄露的隐患：超时之后，由于没有 <-ch，这个子协程会阻塞在 ch <- cs
   // 有两种解决方案：
   //    1. 把 ch 改成缓冲形式的 channel：ch := make(chan clientResult, 1)
   //    2. 使用 select + default，结果不能放入 ch 的话，就走 default
   // 样例代码 ：
   // // ch := make(chan struct{}, 1)
   //	ch := make(chan struct{})
   //	timeout := time.Second * 2
   //	go func() {
   //		time.Sleep(time.Second * 4)
   //		/*select {
   //		case ch <- struct{}{}:
   //		default:
   //		}*/
   //		ch <- struct{}{}
   //		fmt.Println("协程正常退出")
   //	}()
   //	select {
   //	case <-time.After(timeout):
   //		fmt.Println("超时")
   //	case <-ch:
   //		fmt.Println("正常退出")
   //	}
   //	for{
   //
   //	}

    go func(){
		client, err	
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

```



反射关键函数总结

```
// 获取值
v.Int():   Int, Int8, Int16, Int32, Int64: 
v.Unit():
v.String():
v.Bool():

Slice, Array:
	i < v.Len()
	v.Index(i)  // 修改i位的elem，会反馈到整个array或slice上吗？

Struct:
	i < v.NumField()
	v.Field(i)  // Value类型的i'th field of the struct v， 包含整个fieldName和fieldValue吗？
	v.Type().Field(i).Name --> fieldName
	v.FieldByName("xxx")

Map:
	range _, key := v.MapKeys()  --> key
	v.MapIndex(key)              --> value，还是整体？

Ptr:
	v.Elem()        --> *p

Interface:
	v.Elem()        --> dynamic value


// 设置值
v.Set(reflect.Zero(v.Type))
v.SetString()
v.SetInt()

array:
	v.Index(i)
Slice:
	item := reflect.New(v.Type().Elem()).Elem()  // New()创建一个元素类型的指针，再Elem获取变量
	v.Set(reflect.Append(v, item))
	
Map:
	key := reflect.New(v.Type().Key()).Elem() // Type().Key()返回kind为map的中的元素type
	value := reflect.New(v.Type().Elem()).Elem()
	v.SetMapIndex(key, value)
	

// 获取方法
i < v.NumMethod()
v := reflect.ValueOf(x)
t := v.Type()
methType := v.Method(i).Type()
t.Method(i).Name
methType.String()

```