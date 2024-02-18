# go-RPC

## Introduction
- This is a go RPC library implemented with persistent socket connection. 
- Features: 
  - Supports high concurrency 
  - Supports user-defined Type in RPC arguments and return

## Performance

- The maximum concurrency for integer addition RPC on the Apple M2 chip is approximately `48,000` per second.

## Usage Example

```go
package rpc

type Integer int // define your type
type Math struct { // define your interface
    Add func(Integer, Integer) (Integer, RemoteObjectError) // Note: the last return type must be RemoteObjectError
}

type SimpleMath struct{} // define your implementation
func (sm *SimpleMath) Add(a, b Integer) (Integer, RemoteObjectError) {
    return a+b, RemoteObjectError{"ok"}
}

func TestSimpleAdd(t *testing.T)  {
    // make RPC server
    ifc := &Math{} // interface
    impl := &SimpleMath{} // implementation
    addr := "127.0.0.1:1234"
    service, _ := NewService(ifc, impl, 1234)
    service.Start()
    // make RPC client stub
    StubFactory(ifc, addr, nil)
    
    // do RPC requests
    Concurrent := 48000
    wg := sync.WaitGroup{}
    for i := 0; i < Concurrent; i++ {
        wg.Add(1)
        go func() {
            a, b := Integer(rand.Int()%100), Integer(rand.Int()%100)
            c, err := ifc.Add(a, b)
            fmt.Printf("%v+%v=%v, error=%v\n", a, b, c,err.Error())
            wg.Done()
        }()
    }
    wg.Wait()
}
```
- Please refer to `rpc/rpc_test.go` for more examples

## Test
```shell
make test
```