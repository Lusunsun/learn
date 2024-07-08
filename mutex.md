1、基本含义
1.1 互斥锁: 资源上锁后既不可读也不可写
1.2 读写锁: 资源上锁后可读但不可写

2、互斥锁使用

`func Test_lock(t *testing.T) {
	lock := sync.Mutex{}
	if lock.TryLock() {
		println("上锁成功")
	}
	
	// todo something
	
	defer func() {
		lock.Unlock()
		println("锁释放")
	}()
}`

3、源码解析
// 互斥锁数据结构
`type Mutex struct {
	state int32 // 锁的当前状态  前29位代表等待协程数量， 第30位代表是否处于饥饿状态  31位代表是否有唤醒的写成 32位代表当前是否已上锁
	sema  uint32 // 信号量
}`

// state状态及锁的状态  一共32位  
const (
	mutexLocked = 1 << iota // mutex is locked   001  已上锁
	mutexWoken  // 010 // 有唤醒的goroutine
	mutexStarving // 100 // 饥饿状态 正常模式下优先当前的协程获取到锁，因此队列中等待中的协程可能一直拿不到锁， 此时锁会被标记为饥饿状态， 优先取用等待队列中的锁
	// 0000 0000 0000 0000 0000 0000 0000 0001  已上锁 没有等待者
	// 0000 0000 0000 0000 0000 0000 0000 1001  已上锁 且有1位等待者
	// 0000 0000 0000 0000 0000 0000 0001 0001  已上锁 且有2位等待者
	mutexWaiterShift = iota  // 29位 等待者数量，用State的前29位表示
	starvationThresholdNs = 1e6
)

func (m *Mutex) Lock() {
	// Fast path: grab unlocked mutex.
	if atomic.CompareAndSwapInt32(&m.state, 0, mutexLocked) { // 原子操作 设置状态为mutexLocked上锁状态
		if race.Enabled { // 竞争检测相关  不必关注
			race.Acquire(unsafe.Pointer(m))
		}
		return
	}
	// Slow path (outlined so that the fast path can be inlined)
	m.lockSlow()
}

// TryLock tries to lock m and reports whether it succeeded.
//
// Note that while correct uses of TryLock do exist, they are rare,
// and use of TryLock is often a sign of a deeper problem
// in a particular use of mutexes.
func (m *Mutex) TryLock() bool {
	old := m.state
	if old&(mutexLocked|mutexStarving) != 0 { // 已被上锁 直接返回
		return false
	}

	// There may be a goroutine waiting for the mutex, but we are
	// running now and can try to grab the mutex before that
	// goroutine wakes up.
	if !atomic.CompareAndSwapInt32(&m.state, old, old|mutexLocked) { // 上锁操作
		return false
	}

	if race.Enabled {
		race.Acquire(unsafe.Pointer(m))
	}
	return true // 返回上锁成功
}

func (m *Mutex) lockSlow() {
	var waitStartTime int64 // 自旋开始时间
	starving := false // 是否饥饿
	awoke := false // 标记是已唤醒goroutine 避免唤醒多个goroutine
	iter := 0  // 用来存当前goroutine的自旋次数
	old := m.state // 当前锁的状态
	for {
		// Don't spin in starvation mode, ownership is handed off to waiters
		// so we won't be able to acquire the mutex anyway.
		if old&(mutexLocked|mutexStarving) == mutexLocked && runtime_canSpin(iter) {
			// Active spinning makes sense.
			// Try to set mutexWoken flag to inform Unlock
			// to not wake other blocked goroutines.
			if !awoke && old&mutexWoken == 0 && old>>mutexWaiterShift != 0 &&
				atomic.CompareAndSwapInt32(&m.state, old, old|mutexWoken) {
				awoke = true
			}
			runtime_doSpin()
			iter++
			old = m.state
			continue
		}
		new := old
		// Don't try to acquire starving mutex, new arriving goroutines must queue.
		if old&mutexStarving == 0 {
			new |= mutexLocked
		}
		if old&(mutexLocked|mutexStarving) != 0 {
			new += 1 << mutexWaiterShift
		}
		// The current goroutine switches mutex to starvation mode.
		// But if the mutex is currently unlocked, don't do the switch.
		// Unlock expects that starving mutex has waiters, which will not
		// be true in this case.
		if starving && old&mutexLocked != 0 {
			new |= mutexStarving
		}
		if awoke {
			// The goroutine has been woken from sleep,
			// so we need to reset the flag in either case.
			if new&mutexWoken == 0 {
				throw("sync: inconsistent mutex state")
			}
			new &^= mutexWoken
		}
		if atomic.CompareAndSwapInt32(&m.state, old, new) {
			if old&(mutexLocked|mutexStarving) == 0 {
				break // locked the mutex with CAS
			}
			// If we were already waiting before, queue at the front of the queue.
			queueLifo := waitStartTime != 0
			if waitStartTime == 0 {
				waitStartTime = runtime_nanotime()
			}
			runtime_SemacquireMutex(&m.sema, queueLifo, 1)
			starving = starving || runtime_nanotime()-waitStartTime > starvationThresholdNs
			old = m.state
			if old&mutexStarving != 0 {
				// If this goroutine was woken and mutex is in starvation mode,
				// ownership was handed off to us but mutex is in somewhat
				// inconsistent state: mutexLocked is not set and we are still
				// accounted as waiter. Fix that.
				if old&(mutexLocked|mutexWoken) != 0 || old>>mutexWaiterShift == 0 {
					throw("sync: inconsistent mutex state")
				}
				delta := int32(mutexLocked - 1<<mutexWaiterShift)
				if !starving || old>>mutexWaiterShift == 1 {
					// Exit starvation mode.
					// Critical to do it here and consider wait time.
					// Starvation mode is so inefficient, that two goroutines
					// can go lock-step infinitely once they switch mutex
					// to starvation mode.
					delta -= mutexStarving
				}
				atomic.AddInt32(&m.state, delta)
				break
			}
			awoke = true
			iter = 0
		} else {
			old = m.state
		}
	}

	if race.Enabled {
		race.Acquire(unsafe.Pointer(m))
	}
}

// Unlock unlocks m.
// It is a run-time error if m is not locked on entry to Unlock.
//
// A locked Mutex is not associated with a particular goroutine.
// It is allowed for one goroutine to lock a Mutex and then
// arrange for another goroutine to unlock it.
func (m *Mutex) Unlock() {
	if race.Enabled {
		_ = m.state
		race.Release(unsafe.Pointer(m))
	}

	// Fast path: drop lock bit.
	new := atomic.AddInt32(&m.state, -mutexLocked) // 状态字段-1
	if new != 0 { // -1后如果还有其他状态  例如等待队列数量不为0  则需要唤醒队列中的g
		// Outlined slow path to allow inlining the fast path.
		// To hide unlockSlow during tracing we skip one extra frame when tracing GoUnblock.
		m.unlockSlow(new)
	}
}

func (m *Mutex) unlockSlow(new int32) {
	// 判断是否解锁了一个未上锁的锁，new+mutexLocked 理论上应该为1， & 1 应为1
	// 为什么不直接判断 new+mutexLocked == 0 ？？？在并发编程中，使用按位操作（如按位与 & 和按位或 |）来代替简单的相等判断，可以提供更好的多线程安全性和正确性
	if (new+mutexLocked)&mutexLocked == 0 { 
		fatal("sync: unlock of unlocked mutex")
	}
	if new&mutexStarving == 0 { // 非饥饿状态
		old := new
		for {

			// 如果说锁没有等待拿锁的goroutine
			// 或者锁被获取了(在循环的过程中被其它goroutine获取了)
			// 或者锁是被唤醒状态(表示有goroutine被唤醒，不需要再去尝试唤醒其它goroutine)
			// 或者锁是饥饿模式(会直接转交给队列头的goroutine)
			// 那么就直接返回，啥都不用做了

			// 也就是没有等待的goroutine, 或者锁不处于空闲的状态，直接返回.
			if old>>mutexWaiterShift == 0 || old&(mutexLocked|mutexWoken|mutexStarving) != 0 {
				return
			}

			// 有阻塞的goroutine，唤醒一个或变为没有阻塞的goroutine了就退出
			// 这个被唤醒的goroutine还需要跟新来的goroutine竞争
			// 如果只剩最后一个被阻塞的goroutine。唤醒它之后，state就变成0。
			// 如果此刻来一个新的goroutine抢锁，它有可能在goroutine被重新调度之前抢锁成功。
			// 这样就失去公平性了，不能让它那么干，所以这里也要设置为woken模式。
			// 因为Lock方法开始的fast path，CAS操作的old值是0。这里设置woken模式成功后，后来者就只能乖乖排队。保持了锁的公平性
			new = (old - 1<<mutexWaiterShift) | mutexWoken
			if atomic.CompareAndSwapInt32(&m.state, old, new) {
				runtime_Semrelease(&m.sema, false, 1)
				return
			}
			old = m.state
		}
	} else {
		// 饥饿模式
    	// 手递手唤醒一个goroutine
		runtime_Semrelease(&m.sema, true, 1)
	}
}

4、关键逻辑
4.1 Mute.state字段的含义:  高29位 标记等待g的个数，从左到右第三位 是否为饥饿状态，从左到右第二位是否存在已唤醒g  从左到右第一位是否锁定
4.2 自旋: 如果锁已被占且可以自旋则当前g进入自旋状态，自旋时锁被释放且非饥饿状态 则当前g直接拿到锁 避免上下文切换 提升效率
4.3 锁是否进入饥饿模式  Mute.mutexStarving: 正常模式下优先当前的协程获取到锁，因此队列中等待中的协程可能一直拿不到锁， 此时锁会被标记为饥饿状态， 优先将锁交给等待队列中第一个g
4.4 是否存在已被唤醒的goroutine  Mute.mutexWoken: 是否已有被唤醒的g 避免重复唤醒
4.5 m.sema信号量

在Go语言中，信号量（`sema`）是用来控制协程（goroutine）的休眠和唤醒的基本同步原语。Go语言运行时提供了一些低级别的原子操作和信号量机制来实现这种控制。这里我们详细解释一下这些机制及其工作原理。

### 信号量的基本概念

信号量是一种用于限制资源访问数量的同步机制。它维护一个计数器，表示当前可用资源的数量：

- **P操作（wait/pend）**：请求资源，如果资源可用（计数器大于0），则分配资源（计数器减1）；否则，进入休眠状态，等待资源变为可用。
- **V操作（signal/post）**：释放资源（计数器加1），如果有等待的进程或线程，则唤醒一个等待的进程或线程。

### Go语言中的信号量

在Go语言中，信号量的操作主要通过`runtime`包中的一些内部函数来实现。以下是Go语言中使用信号量来控制协程休眠和唤醒的一些关键步骤：

1. **休眠（等待资源）**：
   - 当一个协程尝试获取资源但资源不可用时，它会进入休眠状态。
   - 休眠通过调用 `runtime_Semacquire` 函数实现，这个函数会使当前协程进入阻塞状态，直到信号量可用。

2. **唤醒（释放资源）**：
   - 当资源变得可用时，信号量计数器增加，并唤醒等待资源的协程。
   - 唤醒通过调用 `runtime_Semrelease` 函数实现，这个函数会增加信号量计数器并唤醒一个阻塞的协程。

### 具体实现示例

以下是一个简单的Go语言信号量控制协程休眠和唤醒的示例：

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"runtime"
	"time"
)

var sema uint32

func worker(id int, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Printf("Worker %d waiting for the signal\n", id)
	runtime_Semacquire(&sema)
	fmt.Printf("Worker %d got the signal\n", id)
}

func main() {
	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go worker(i, &wg)
	}

	time.Sleep(2 * time.Second)
	fmt.Println("Releasing the semaphore")
	runtime_Semrelease(&sema)

	wg.Wait()
	fmt.Println("All workers done")
}

// runtime package functions
func runtime_Semacquire(sema *uint32) {
	for {
		if atomic.LoadUint32(sema) > 0 && atomic.CompareAndSwapUint32(sema, 1, 0) {
			return
		}
		runtime.Gosched() // Yield the processor to allow other goroutines to run
	}
}

func runtime_Semrelease(sema *uint32) {
	atomic.StoreUint32(sema, 1)
}
```

### 解释

1. **休眠**：
   - 在 `worker` 函数中，每个协程调用 `runtime_Semacquire(&sema)` 来等待信号量。
   - `runtime_Semacquire` 函数通过循环检查信号量的值，并使用原子操作来确保安全的并发访问。如果信号量不可用，协程会主动放弃CPU时间片，通过 `runtime.Gosched()` 函数使其他协程有机会运行。

2. **唤醒**：
   - 在 `main` 函数中，主协程等待2秒后调用 `runtime_Semrelease(&sema)` 来释放信号量。
   - `runtime_Semrelease` 函数通过原子操作增加信号量的值，并使一个等待的协程恢复运行。

### 总结

通过使用信号量和原子操作，Go语言能够有效地控制协程的休眠和唤醒，从而实现并发编程中的同步与互斥。这些机制在Go语言运行时的底层实现中被广泛使用，确保了协程的高效调度和资源管理。

https://www.cnblogs.com/ricklz/p/14535653.html
https://cbsheng.github.io/posts/%E4%B8%80%E4%BB%BD%E8%AF%A6%E7%BB%86%E6%B3%A8%E9%87%8A%E7%9A%84go-mutex%E6%BA%90%E7%A0%81/







