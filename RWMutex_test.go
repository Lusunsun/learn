package learn

import (
	"testing"
	"sync"
)


func Test_RWMutex(t *testing.T) {
	a := mutexWaiterShift
	println(a)
	lock := sync.RWMutex{}
	if lock.TryLock() {
		println("上锁成功")
	}
	
	// todo something
	
	defer func() {
		lock.Unlock()
		println("锁释放")
	}()
}
