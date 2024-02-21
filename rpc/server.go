package rpc

import (
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
)

// RemoteObjectError is a custom error type used for this library to identify remote methods.
// it is used by both caller and callee endpoints.
type RemoteObjectError struct {
	Err string
}

// getter for the error message included inside the custom error type
func (e *RemoteObjectError) Error() string { return e.Err }

type ReplyMsg struct {
	Success bool
	Reply   []interface{}
	Seq     uint64
	Err     RemoteObjectError
}

// Service RPC server
type Service struct {
	functions map[string]reflect.Value // map function prototype to function
	address   string
	running   uint32 // 1: running, 0: stopped
	numCalls  uint32
	listener  net.Listener
	hPool     *Pool
}

// register the interface and its implementation
func (serv *Service) register(ifc interface{}, impl interface{}) error {
	if ifc == nil || impl == nil {
		return errors.New("interface or implementation is nil")
	}
	var ifcVal reflect.Value
	var ifcType reflect.Type
	var implVal reflect.Value
	var implType reflect.Type
	ifcVal = reflect.ValueOf(ifc)
	ifcType = reflect.TypeOf(ifc)
	implVal = reflect.ValueOf(impl)
	implType = reflect.TypeOf(impl)
	if reflect.TypeOf(ifc).Kind() == reflect.Pointer {
		ifcVal = ifcVal.Elem()
		ifcType = ifcType.Elem()
	}
	if reflect.TypeOf(impl).Kind() == reflect.Pointer {
		implVal = implVal.Elem()
		implType = implType.Elem()
	}
	name2val := make(map[string]reflect.Value)
	// load all functions from the interface
	for i := 0; i < ifcVal.NumField(); i++ {
		fnV := ifcVal.Field(i)
		// check if last return argument is RemoteObjectError
		err := checkLastReturnType(fnV)
		if err != nil {
			return err
		}
		// map prototype to an empty
		//fnName := ifcType.Field(i).Name
		prototype := getFuncPrototypeByStructField(ifcType.Field(i))
		//fmt.Println(prototype)
		if prototype == "" {
			return errors.New("unexpected: function name is empty")
		}
		name2val[prototype] = reflect.ValueOf(nil)
	}

	// set function's implementations
	for i := 0; i < reflect.TypeOf(impl).NumMethod(); i++ {

		fnVal := reflect.ValueOf(impl).Method(i)
		fnType := reflect.TypeOf(impl).Method(i)
		prototype := getFuncPrototypeByFuncType(fnType.Name, fnVal.Type())
		if _, ok := name2val[prototype]; !ok {
			continue // this function is not belong to the interface
		}
		// check if last return argument is RemoteObjectError
		err := checkLastReturnType(fnVal)
		if err != nil {
			return err
		}

		// resister all argument/return types into gob
		for j := 0; j < fnType.Type.NumIn(); j++ { // register types in gob (for type struct)
			argType := fnType.Type.In(j)
			obj := reflect.New(argType).Elem().Interface()
			gob.Register(obj)
		}
		for j := 0; j < fnType.Type.NumOut(); j++ { // register types in gob (for type struct)
			argType := fnType.Type.Out(j)
			obj := reflect.New(argType).Elem().Interface()
			gob.Register(obj)
		}

		name2val[prototype] = fnVal // function's reflect.Value
	}

	// check whether a prototype is not implemented
	for fnName, fnValue := range name2val {
		if fnValue == reflect.ValueOf(nil) {
			errMsg := fmt.Sprintf("function with name %v is not implemented", fnName)
			return errors.New(errMsg)
		}
	}
	serv.functions = name2val
	return nil
}

// checkLastReturnType check whether the function's last return type is RemoteObjectError
func checkLastReturnType(fnV reflect.Value) error {
	expectType := reflect.TypeOf(RemoteObjectError{})
	numOut := fnV.Type().NumOut()
	if numOut == 0 {
		return errors.New("function's last return type is not RemoteObjectError")
	}
	lastOutType := fnV.Type().Out(numOut - 1)
	if lastOutType.Name() != expectType.Name() || lastOutType.PkgPath() != expectType.PkgPath() {
		return errors.New("function's last return type is not RemoteObjectError")
	}
	return nil
}

// NewService build a new RPC server
func NewService(ifc interface{}, impl interface{}, port int) (*Service, error) {
	service := Service{
		functions: make(map[string]reflect.Value),
		address:   "0.0.0.0:" + strconv.Itoa(port),
		running:   0,
		numCalls:  0,
		listener:  nil,
		hPool:     NewPool(4),
	}
	err := service.register(ifc, impl)
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// readRequest read a single RPC request from the socket connection, return the decoded
func (serv *Service) readRequest(conn net.Conn) (*RequestMsg, error) {
	// read header
	//header := make([]byte, 4)
	ptr := serv.hPool.Get()
	header := ptr.content
	nRead, err := io.ReadFull(conn, header)
	if err != nil || nRead == 0 {
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(header))
	serv.hPool.Put(ptr)
	// read body
	body := make([]byte, size)
	nRead, err = io.ReadFull(conn, body)
	if err != nil || nRead == 0 {
		return nil, err
	}

	// decode bytes to RequestMsg
	req, err := DecodeToReqMsg(body)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (serv *Service) writeResponse(conn net.Conn, reply ReplyMsg) error {
	// encode
	body, err := EncodeToBytes(reply)
	if err != nil {
		return errors.New("encode ReplyMsg failed")
	}
	data := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(data[:4], uint32(len(body)))
	copy(data[4:], body)

	// write it to rpc client
	_, err = conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}

// Accept connections on listener and serve the request.
// Expect to invoke Accept in the go statement
func (serv *Service) Accept(lis net.Listener) {
	for serv.IsRunning() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		//! go it
		go serv.ServeConn(conn)
	}
}

// ServeConn serve a socket connection.
// Close connection after all requests are done
// ServeConn serve a socket connection.
// Close connection after all requests are done
func (serv *Service) ServeConn(conn net.Conn) {
	log.Printf("server remote=%v, local=%v\n", conn.RemoteAddr(), conn.LocalAddr())
	wg := new(sync.WaitGroup)
	for serv.IsRunning() {
		// read a RequestMsg from the sio
		reqMsg, err := serv.readRequest(conn)
		if err != nil { // e.g., EOF
			break
		}
		// call correspondent function
		wg.Add(1)
		//! go it
		go serv.Call(reqMsg, conn, wg)
	}
	wg.Wait()
	err := conn.Close()
	if err != nil {
		log.Println("close conn failed")
	}
	log.Println("serve conn done!")
}

// Call the corresponding function based on RequestMsg, and then write the return values into SocketIO
func (serv *Service) Call(req *RequestMsg, conn net.Conn, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	if req == nil {
		return
	}

	// call function
	args := Interfaces2Values(req.Args)
	fnVal, exist := serv.functions[req.Prototype]
	var reply ReplyMsg
	if !exist {
		reply = ReplyMsg{
			Success: false,
			Reply:   nil,
			Seq:     req.Seq,
			Err:     RemoteObjectError{fmt.Sprintf("error: function with prototype '%v' does not exist", req.Prototype)},
		}
	} else {
		returnValues := fnVal.Call(args)
		reply = ReplyMsg{
			Success: true,
			Reply:   Values2Interfaces(returnValues),
			Seq:     req.Seq,
			Err:     RemoteObjectError{"ok"},
		}
	}

	// write response to client
	err := serv.writeResponse(conn, reply)
	if err != nil {
		log.Printf("write response err: %v", err.Error())
		return
	}
	atomic.AddUint32(&serv.numCalls, 1)

}

// Start the Service's tcp listening connection, update the Service
// status, and start receiving caller connections
func (serv *Service) Start() error {
	if serv.IsRunning() {
		log.Print("serv.Start: warning, service has already started")
		return nil
	}
	//atomic.StoreUint32(&serv.running, 1) // ! uncomment it in production
	lis, err := net.Listen("tcp", serv.address)
	if err != nil {
		log.Print("serv.Start: listen failed, ", err.Error())
		return err
	}
	serv.listener = lis
	//! go it
	go serv.Accept(lis)
	// set running
	atomic.StoreUint32(&serv.running, 1)

	return nil
}

// GetCount returns the number of successful RPC
func (serv *Service) GetCount() int {
	// the total number of remote calls served successfully by this Service
	num := atomic.LoadUint32(&serv.numCalls)
	return int(num)
}

// IsRunning return a boolean value indicating whether the Service is running
func (serv *Service) IsRunning() bool {
	return atomic.LoadUint32(&serv.running) == 1
}

// Stop the service: set running to be 0 and close the listener
func (serv *Service) Stop() {
	atomic.StoreUint32(&serv.running, 0)
	err := serv.listener.Close()
	if err != nil {
		log.Println("close listener failed")
		return
	}
}
