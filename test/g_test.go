package test

import (
	"testing"
	"time"
	"sync"
	"errors"
	"fmt"
)

func Test_c(t *testing.T) {
	overChan := make(chan struct{})
	close(overChan)
	for i := 0; i < 10; i++ {
		select {
		case <-overChan:
			println("over")
		}
	}
}

func Test_g(t *testing.T) {
	var (
		err error
	)
	
	wg := sync.WaitGroup{}
	errChan := make(chan error)
	overChan := make(chan struct{})
	
	go func() {
		defer close(overChan)
		for err2 := range errChan {
			err = err2
		}
	}()
	
	res := make([]int64, 0)
	for i := 1; i < 100; i++ {
		select {
		case <-overChan:
			break
		default:
			wg.Add(1)
			go func(data int64) {
				defer wg.Done()
				workRes, err := work(data)
				if err != nil {
					errChan <- err
				}
				select {
				case <-overChan:
					break
				default:
					res = append(res, workRes)
				}
			}(int64(i))
		}
	}
	wg.Wait()
	close(errChan)
	<-overChan
	if err != nil {
		println(err.Error())
	}
	
	println(len(res))
}

func work(i int64) (int64, error) {
	time.Sleep(50 * time.Millisecond)
	if i == 10 {
		return 0, errors.New(fmt.Sprintf("err: %d", i))
	}
	return i, nil
}
