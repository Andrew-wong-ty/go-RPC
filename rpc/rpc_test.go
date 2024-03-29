package rpc

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// type Integer int // define your type
type Math struct { // define your interface
	Add func(int, int) (int, RemoteObjectError)
}

type SimpleMath struct{} // define your implementation

func (sm *SimpleMath) Add(a, b int) (int, RemoteObjectError) {
	return a + b, RemoteObjectError{"ok"}
}

func TestSimpleAdd(t *testing.T) {

	// make RPC server
	ifc := &Math{}        // interface
	impl := &SimpleMath{} // implementation
	addr := "127.0.0.1:1234"
	service, _ := NewService(ifc, impl, 1234)
	service.Start()
	// make RPC client stub
	StubFactory(ifc, addr, nil)

	// do RPC requests
	Concurrent := 1000000
	wg := sync.WaitGroup{}
	for i := 0; i < Concurrent; i++ {
		wg.Add(1)
		go func() {
			a, b := rand.Int()%100, rand.Int()%100
			ifc.Add(a, b)
			//c, err := ifc.Add(a, b)
			//fmt.Printf("%v+%v=%v, error=%v\n", a, b, c,err.Error())
			wg.Done()
		}()
	}
	wg.Wait()
}

type Args struct {
	A, B int
}

type Arith int

func (t *Arith) Add(args *Args, reply *int) error {
	*reply = args.A + args.B
	return nil
}
func TestNETRPC(t *testing.T) {
	simpleMath := new(Arith)
	rpc.Register(simpleMath)
	rpc.HandleHTTP()
	l, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal("listen error:", err)
	}
	go http.Serve(l, nil)

	client, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	if err != nil {
		log.Fatal("dialing:", err)
	}

	// do RPC requests
	Concurrent := 50000
	wg := sync.WaitGroup{}
	for i := 0; i < Concurrent; i++ {
		wg.Add(1)
		go func() {
			args := &Args{
				A: int(rand.Int() % 100),
				B: int(rand.Int() % 100),
			}
			var reply int
			err = client.Call("Arith.Add", args, &reply)
			//fmt.Printf("%v+%v=%v, error=%v\n", a, b, c,err.Error())
			wg.Done()
		}()
	}
	wg.Wait()
}

// Point  Note: type used in RPC must be exported and has exported fields
type Point struct {
	X float64
	Y float64
}

type MyInterface struct {
	Add    func(Point, Point) (Point, RemoteObjectError)
	Divide func(int, int) (int, RemoteObjectError)
}

type MyImplementation struct{}

func (i *MyImplementation) Add(p1, p2 Point) (Point, RemoteObjectError) {
	p := Point{
		X: p1.X + p2.X,
		Y: p1.Y + p2.Y,
	}
	// random sleep
	random := rand.Float64()
	if random < 0.5 {
		time.Sleep(1 * time.Second)
	}

	return p, RemoteObjectError{"ok"}
}

func (i *MyImplementation) Divide(x, y int) (int, RemoteObjectError) {
	if y == 0 {
		return -1, RemoteObjectError{"dividend must not be zero"}
	}
	return x / y, RemoteObjectError{"ok"}
}

func TestConcurrentAdd(t *testing.T) {
	ifc := &MyInterface{}
	impl := &MyImplementation{}
	// make server
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)
	service, _ := NewService(ifc, impl, port)
	service.Start()

	// make stub and set timeout(can be nil)
	timeout := 10 * time.Millisecond
	StubFactory(ifc, addr, &timeout)
	wg := sync.WaitGroup{}

	// call rpc
	N := int32(100) // Concurrent count
	Succeed := N    // N of success rpc
	for i := 0; i < int(N); i++ {
		wg.Add(1)
		go func(order int) {
			p1 := Point{
				X: float64(rand.Int() % 20),
				Y: float64(rand.Int() % 20),
			}
			p2 := Point{
				X: float64(rand.Int() % 20),
				Y: float64(rand.Int() % 20),
			}
			p, err := ifc.Add(p1, p2)
			if err.Err != "ok" {
				atomic.AddInt32(&Succeed, -1)
			}
			fmt.Printf("%v-th (%v, %v) + (%v, %v) = (%v, %v), error=%v\n", order, p1.X, p1.Y, p2.X, p2.Y, p.X, p.Y, err.Err)
			wg.Done()
		}(i)
	}
	wg.Wait()
	service.Stop()
	fmt.Printf("rpc success rate:%v\n", float64(Succeed)/float64(N))
}
