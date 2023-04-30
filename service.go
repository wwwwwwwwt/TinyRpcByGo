/*
 * @Author: zzzzztw
 * @Date: 2023-04-29 21:26:15
 * @LastEditors: Do not edit
 * @LastEditTime: 2023-04-30 17:41:21
 * @FilePath: /TidyRpcByGo/service.go
 */
package tinyrpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method    reflect.Method //方法本身
	ArgType   reflect.Type   //第一个参数的类型
	ReplyType reflect.Type   //返回值的类型 第二个参数的类型
	numCalls  uint64         //统计方法被调用次数
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value

	//这个值可能是值类型或指针类型

	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem() // 得到值
	}

	return argv
}

func (m *methodType) newReplyv() reflect.Value {

	// 返回值必须是指针类型

	replyv := reflect.New(m.ReplyType.Elem())

	switch m.ReplyType.Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}

	return replyv
}

/*
{
    "ServiceMethod"： "T.MethodName"
    "Argv"："0101110101..." // 序列化之后的字节流
}
*/
type service struct {
	name   string                 // T T.method 中的T
	typ    reflect.Type           // 结构体的类型 T的类型
	rcvr   reflect.Value          // 收到结构体的实例本身， 调用时作为第0个参数
	method map[string]*methodType // 存储结构体T的所有符合条件的方法
}

func newService(rcvr interface{}) *service { // 入参是任意要映射为服务的结构体实例
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr) // 一切基于先得到反射后的实际类型

	/*
		这里使用reflect.Indirect的原因在于无法确定用户传入的s.rcvr类型为结构体还是为指针，如果用户传入的为指针的话，直接采用s.typ.Name()输出的为空字符串
		log.Println(" struct name: " + reflect.TypeOf(foo).Name())//输出Foo
		log.Println("pointer name: " + reflect.TypeOf(&foo).Name())//输出空串
		因此需要采用reflect.Indirect(s.rcvr)方法，提取实例对象再获取名称
	*/
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr) // 通过实例的反射得到结构提的类型，然后通过结构体类型得到method
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// 通过registerMethods方法 过滤出符合条件的方法
//func (t *T) MethodName(argType T1, replyType *T2) error
// 1.自身两个导出或内置类型的入参，（反射时是三个，第0个是自身）
// 2.返回值只有一个 error类型
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)

	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type

		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		argType, replyType := mType.In(1), mType.In(2)

		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

// 通过反射调用方法
func (s *service) call(m *methodType, argv reflect.Value, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func

	// 正常调用是 A.func(argv1, argv2)，反射的时候就是 Call(A, argv1, argv2)。
	returnValue := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValue[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
