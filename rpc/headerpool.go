package rpc

import "sync"

type Buf struct {
	content []byte
	next    *Buf
}

func NewBuf(size int) *Buf {
	return &Buf{
		content: make([]byte, size),
		next:    nil,
	}
}

type Pool struct {
	mutex  sync.Mutex
	size   int
	header *Buf
}

func NewPool(size int) *Pool {
	return &Pool{
		mutex:  sync.Mutex{},
		header: nil,
		size:   size,
	}
}

func (p *Pool) Get() *Buf {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.header == nil {
		return NewBuf(p.size)
	} else {
		ptr := p.header
		p.header = ptr.next
		return ptr
	}

}

func (p *Pool) Put(buf *Buf) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	buf.next = p.header
	p.header = buf
}
