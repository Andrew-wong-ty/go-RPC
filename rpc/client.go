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
	"sync"
	"time"
)

type RequestMsg struct {
	Args      []interface{}
	Prototype string // a string represents a function prototype. e.g., Add:(int, int,)->(int,)
	Seq       uint64
}

type Reply struct {
	prototype string
	args      []interface{}
	returns   []interface{}
	seq       uint64
	done      chan *Reply // to notify a waiting client call is done
}

type Client struct {
	conn    net.Conn
	addr    string
	mutex   sync.Mutex // protects the following
	currSeq uint64     // identifies a call
	pending map[uint64]*Reply
	timeout time.Duration
	hPool   *Pool
}

// writeRequest encode the RequestMsg into bytes and then
// write to the connection
// a data frame is encoded like this:
//
//	+-------+---------+
//	| header|   body  |
//	+-------+---------+
func (c *Client) writeRequest(req RequestMsg) error {
	// body
	payload, err := EncodeToBytes(req)
	if err != nil {
		return errors.New("encode RequestMsg failed")
	}
	// header
	data := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(data[:4], uint32(len(payload)))
	copy(data[4:], payload)
	// write it to rpc server
	_, err = c.conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}

// readResponse reads a single response and decode it into ReplyMsg
func (c *Client) readResponse() (*ReplyMsg, error) {
	// read header
	//header := make([]byte, 4)
	ptr := c.hPool.Get()
	header := ptr.content
	nRead, err := io.ReadFull(c.conn, header)
	if err != nil || nRead == 0 {
		log.Printf("client read header failed")
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(header))
	c.hPool.Put(ptr)
	// read body
	body := make([]byte, size)
	nRead, err = io.ReadFull(c.conn, body)
	if err != nil || nRead == 0 {
		log.Printf("client read body failed")
		return nil, err
	}
	reply, err := DecodeToReplyMsg(body)
	if err != nil {
		return nil, err
	}
	return reply, nil
}

// sendRequest send request to RPC server using SocketIO, which sends function prototype and arguments
func (c *Client) sendRequest(prototype string, args []reflect.Value) (chan *Reply, error) {
	argsIfc := Values2Interfaces(args)
	c.mutex.Lock()
	c.currSeq += 1
	req := RequestMsg{
		Args:      argsIfc,
		Prototype: prototype,
		Seq:       c.currSeq,
	}
	reply := &Reply{
		prototype: prototype,
		args:      argsIfc,
		returns:   nil,
		seq:       c.currSeq,
		done:      make(chan *Reply, 1),
	}
	c.pending[reply.seq] = reply
	c.mutex.Unlock()

	err := c.writeRequest(req)
	if err != nil {
		log.Printf("write request error")
		return nil, err
	}
	return reply.done, nil
}

func (c *Client) readInput() {
	for {
		// read a single response from socket conn
		response, err := c.readResponse()
		if err != nil {
			log.Printf("read response failed, err: %v\n", err.Error())
			break
		}
		c.mutex.Lock()
		reply, exist := c.pending[response.Seq]
		c.mutex.Unlock()
		go func(c *Client) {
			if !exist {
				log.Printf("unexpected: the pending reply(seq=%v) is non-exist\n", response.Seq)
				return
			}
			reply.returns = response.Reply
			// notify
			reply.done <- reply
			// remove entry
			c.mutex.Lock()
			delete(c.pending, reply.seq)
			c.mutex.Unlock()
		}(c)
	}
	log.Println("readInput failed!")
}

// newClient creates a new client
func newClient(addr string, conn net.Conn, timeout *time.Duration) *Client {
	var maxWaitTime time.Duration
	if timeout == nil {
		maxWaitTime = time.Duration(1<<63 - 1) // unlimited time
	} else {
		maxWaitTime = *timeout
	}
	return &Client{
		conn:    conn,
		addr:    addr,
		mutex:   sync.Mutex{},
		currSeq: 0,
		pending: make(map[uint64]*Reply),
		timeout: maxWaitTime,
		hPool:   NewPool(4),
	}
}

// makeZeroResults creates a zero-filled result slice of reflect.Values;
// use it when RPC call fails
func makeZeroResults(fnF reflect.StructField, errMsg string) []reflect.Value {
	zeroRes := make([]reflect.Value, fnF.Type.NumOut())
	for j := 0; j < fnF.Type.NumOut()-1; j++ { // fill all the return type except the last one -- RemoteObjectError
		argType := fnF.Type.Out(j)
		obj := reflect.Zero(argType)
		zeroRes[j] = obj
	}
	zeroRes[fnF.Type.NumOut()-1] = reflect.ValueOf(RemoteObjectError{Err: errMsg})
	return zeroRes
}

// newImplementation uses reflection to provide implementations for functions in Interface
func newImplementation(fn reflect.Value, fnF reflect.StructField, client *Client) reflect.Value {

	//  a function that implements the interface
	clientRPC := func(args []reflect.Value) []reflect.Value {

		prototype := getFuncPrototypeByStructField(fnF)
		done, err := client.sendRequest(prototype, args)
		if err != nil {
			log.Printf("err=%v\n", err.Error())
			return makeZeroResults(fnF, err.Error())
		}

		// block here wait for result
		select {
		case reply := <-done:
			rspIfc := Interfaces2Values(reply.returns)
			return rspIfc
		case <-time.After(client.timeout):
			errMsg := fmt.Sprintf("rpc call %v timeout\n", prototype)
			return makeZeroResults(fnF, errMsg)
		}
	}

	// set function to the interface and do retry when fail
	newFn := reflect.MakeFunc(fn.Type(), func(args []reflect.Value) []reflect.Value {

		refVals := clientRPC(args)
		return refVals
	})
	return newFn
}

//// notFatalErr check which type of error is not fatal. If not fatal, retry
//func notFatalErr(errStr string) bool {
//	return errStr == errReadStr
//}

// StubFactory creates a new RPC client, use reflections implement the interface, and register types in gob
func StubFactory(ifcPtr interface{}, adr string, timeout *time.Duration) (net.Conn, error) {
	if ifcPtr == nil {
		return nil, errors.New("ifcPtr is a null pointer")
	}
	if reflect.TypeOf(ifcPtr).Kind() != reflect.Pointer {
		return nil, errors.New("argument `ifc` should be a pointer")
	}
	conn, err := net.Dial("tcp", adr)
	if err != nil {
		return nil, errors.New("connect to rpc service failed")
	}

	client := newClient(adr, conn, timeout)
	ifv := reflect.ValueOf(ifcPtr)
	ift := reflect.TypeOf(ifcPtr)

	// register argument/return types and implement interfaces
	for i := 0; i < ifv.Elem().NumField(); i++ {
		fnV := ifv.Elem().Field(i)
		fnF := ift.Elem().Field(i)
		err := checkLastReturnType(fnV)
		if err != nil {
			return nil, err
		}
		// register return argument types in gob
		for j := 0; j < fnF.Type.NumIn(); j++ {
			argType := fnF.Type.In(j)
			obj := reflect.New(argType).Elem().Interface()
			gob.Register(obj)
		}
		// register return types in gob
		for j := 0; j < fnF.Type.NumOut(); j++ {
			argType := fnF.Type.Out(j)
			obj := reflect.New(argType).Elem().Interface()
			gob.Register(obj)
		}
		// implement this function interface
		implementation := newImplementation(fnV, fnF, client)
		fnV.Set(implementation)
	}
	go client.readInput()
	return conn, nil
}
