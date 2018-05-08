package refreshing

import (
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var errOutOfRange = errors.New("Index out of range")

func refreshFuncWithData(t *testing.T, data []int) func() (interface{}, error) {
	i := 0
	return func() (interface{}, error) {
		defer func() { i++ }() // increment array index on each function call
		if i >= len(data) {
			return 0, errOutOfRange
		}
		// this sleep emphases that there
		// is no strict synchronization between refreshFunc and Load
		time.Sleep(10 * time.Microsecond)
		// t.Logf("i = %d, data[i] = %d\n", i, data[i])
		return data[i], nil
	}
}

func Test__refreshFunc_updates_value_at_initialization(t *testing.T) {
	a := assert.New(t)

	refreshFunc := refreshFuncWithData(t, []int{123, 456})
	ticker := make(chan time.Time)

	v := New(refreshFunc, ticker)
	err := v.Wait(time.Millisecond * 50)
	a.NoError(err)

	a.Equal(123, v.Load().(int))

}

func Test__refreshFunc_updates_value_on_tick(t *testing.T) {
	a := assert.New(t)
	refreshFunc := refreshFuncWithData(t, []int{123, 456})
	ticker := make(chan time.Time)

	v := New(refreshFunc, ticker) // 123 on init
	err := v.Wait(time.Millisecond * 25)
	a.NoError(err)

	ticker <- time.Now() // 456 after tick
	err = v.Wait(time.Millisecond * 100)
	a.NoError(err)
	a.Equal(456, v.Load().(int))
}

func Test__refreshFunc_does_not_update_value_on_error(t *testing.T) {
	a := assert.New(t)
	refreshFunc := refreshFuncWithData(t, []int{123, 456})

	ticker := make(chan time.Time)

	v := New(refreshFunc, ticker) // 123
	err := v.Wait(time.Millisecond * 50)
	a.NoError(err)

	ticker <- time.Now() // 456 after tick
	err = v.Wait(time.Millisecond * 50)
	a.NoError(err)

	a.Equal(456, v.Load().(int))

	ticker <- time.Now() // 456 - does not update, because of "Index out of range" error
	err = v.Wait(time.Millisecond * 50)
	a.Error(err)
	a.Equal(errOutOfRange, errors.Cause(err))
	a.Equal(456, v.Load().(int))

}

func Test__refreshFunc_slowLog(t *testing.T) {
	a := assert.New(t)

	slowNotified := 0
	var prevSlowDuration time.Duration
	slowFunc := func(t time.Duration) {
		if t < prevSlowDuration {
			return
		}
		slowNotified++
	}

	v := Value{
		refreshFunc: func() (interface{}, error) {
			<-time.After(time.Millisecond * 500)
			return 1, nil
		},
		slowLogFunc:         slowFunc,
		slowLogFuncInterval: time.Millisecond * 11,
	}
	v.store()

	a.InDelta(45, slowNotified, 2)
	slowPrev := slowNotified

	slowNotifiedSecond := 0
	v.ProgressFunc(time.Millisecond*100, func(time.Duration) {
		slowNotifiedSecond++
	})

	v.store()
	a.Equal(slowPrev, slowNotified, "Prev slowFunc shouldn't be used")
	a.InDelta(5, slowNotifiedSecond, 1)

	v2 := New(
		func() (interface{}, error) { return 1, nil },
		make(chan time.Time),
		slowFunc,
	)
	a.Equal(reflect.ValueOf(slowFunc).Pointer(), reflect.ValueOf(v2.slowLogFunc).Pointer(), "SlowLogFunc should be assigned from optional param")

}
