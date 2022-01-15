package tunny

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestFuncGeneric(t *testing.T) {
	pool := NewFunc(10, func(t int) (string, error) {
		return strconv.Itoa(t), nil
	})
	defer pool.Close()

	for i := 0; i < 20; i++ {
		ret, err := pool.Process(i)
		if err != nil {
			t.Errorf("Should not got error: %v", err)
		}
		if exp, act := strconv.Itoa(i), ret; exp != act {
			t.Errorf("Wrong result: %v != %v", exp, act)
		}
	}
}

func TestFuncGenericErrorTimeout(t *testing.T) {
	pool := NewFunc(10, func(t int) (string, error) {
		<-time.After(time.Millisecond * 50)
		return strconv.Itoa(t), nil
	})
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	inp := 10
	exp := strconv.Itoa(inp)

	act, err := pool.ProcessCtx(ctx, inp)
	if exp := context.DeadlineExceeded; err != exp {
		t.Errorf("Should got error: %v != %v", exp, err)
	}
	if exp == act {
		t.Errorf("Should not match: %+#v == %+#v", exp, act)
	}
}
