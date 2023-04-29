<!--
 * @Author: zzzzztw
 * @Date: 2023-04-27 18:33:45
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-29 10:53:22
 * @FilePath: /TidyRpcByGo/README.md
-->
# TinyRpcByGo
  
* bug1:客户端一开始发送json格式Option时 

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

由于没有确定边界，所以可能json把Header的内容读出，导致Header内容缺失，形成阻塞


```

* server端解析Option的时候可能会破坏后面RPC消息的完整性，当客户端消息发送过快服务端消息积压时（例：Option|Header|Body|Header|Body），服务端使用json解析Option，json.Decode()调用conn.read()读取数据到内部的缓冲区（例：Option|Header），此时后续的RPC消息就不完整了(Body|Header|Body)。  
目前代码中客户端简单的使用time.sleep()方式隔离协议交换阶段与RPC消息阶段，减少这种问题发生的可能。