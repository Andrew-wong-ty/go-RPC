# go-RPC

## Introduction
- This is a go RPC library implemented with persistent socket connection. 
- Features: 
  - Supports high concurrency 
  - Supports user-defined Type in RPC arguments and return

## Performance

- The concurrency performance for integer addition RPC on the Apple M2 chip is approximately `48,000` per second.

## Architecture
![image](https://github.com/Andrew-wong-ty/go-RPC/assets/78400045/32d098f2-08f9-432c-9a91-fbe5b3f0f197)
The image illustrates how it works by using an example.
- It first creates a `Math` Interface and implementation. The RPC client registers the interface and the RPC server registers the interface and its implementation. Once the server is running, the client can establish a connection by dialing it.
- When the client invokes the `Add` function, which adds two numbers, the client creates a unique sequence number `seq` and encodes `Add`'s arguments and function name into bytes (`body`). Then the header encodes the `body`'s size. Then the `header`+`body` as a whole is written to the `conn`. It then creates a `channel` for this function to wait until returns are received or timeout.
- Then on the server side, it reads the `body` and decodes out arguments and the function name. A goroutine uses the name to look up the correspondent function's reflection value and call it using arguments. Then the arguments are encoded and packaged in the same way to send to the client.
- Finally, on the client side, a goroutine decodes out a `body` consisting of return values, the sequence number `seq`, etc. Then `seq` is used to look up the previously mentioned `channel`. Returns are sent to this `channel`, and the function receives it and finally returns the result of `Add`.

Note: the previous description simplified the process to convey the general idea, which differs from the actual implementation.


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
    service, _ := NewService(ifc, impl, 1234)
    service.Start()
    // make RPC client stub
    StubFactory(ifc, "127.0.0.1:1234", nil)
    
    // do RPC requests
    concurrency := 48000
    wg := sync.WaitGroup{}
    for i := 0; i < concurrency; i++ {
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

## Optimization
- Every time we parse the RPC data sent to the client/server, we need a 4-size `[]byte` to obtain the RPC `body` size, which is a fixed overhead. 
So we can create a pool to store these `[]byte` objects to 
reduce GC times. Please refer to `headerpool.go`.

## Test
```shell
make test
```
