package learn

import (
	"testing"
	"sync"
	"time"
)


func Test_RWMutex(t *testing.T) {
	lock := sync.RWMutex{}
	lock.Lock()
	println("上锁成功")
	
	go func() {
		lock.Lock()
		println("上锁成功2")
		time.Sleep(time.Minute)
		defer func() {
			lock.Unlock()
			println("锁释放2")
		}()
	}()
	
	time.Sleep(time.Minute)
	
	
	
	defer func() {
		lock.Unlock()
		println("锁释放")
	}()
}
