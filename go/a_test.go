package _go

import (
	"time"
	"sync"
	"log"
	"testing"
)

var done = false

func read(name string, c *sync.Cond) {
	c.L.Lock()
	println(name, "lock")
	if !done {
		c.Wait()
	}
	log.Println(name, "starts reading")
	c.L.Unlock()
}

func write(name string, c *sync.Cond) {
	log.Println(name, "starts writing")
	time.Sleep(time.Second)
	c.L.Lock()
	done = true
	c.L.Unlock()
	log.Println(name, "wakes all")
	c.Broadcast()
}

func Test_Cond(t *testing.T) {
	
	a := make(chan int)
	close(a)
	
	cond := sync.NewCond(&sync.Mutex{})

	go read("reader1", cond)
	go read("reader2", cond)
	go read("reader3", cond)
	write("writer", cond)

	time.Sleep(time.Second * 3)
}

