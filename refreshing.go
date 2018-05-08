/*Package refreshing provides facility that retrieves and caches the value
returned from any function.

Value can be easily integrated with time.Ticker which would provide refresh by TTL.
*/
package refreshing

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

// A Value provides auto-refreshing value based on sync/atomic.Value.
// If refreshFunc returns an error - last successfully retrieved value
// is returned by .Load().
type Value struct {
	refreshFunc func() (interface{}, error)
	atomicValue atomic.Value

	// valueNotificator receives true value each time a value is refreshed.
	valueNotificator chan bool

	slowLogFunc         func(elapsed time.Duration)
	slowLogFuncInterval time.Duration
	slowLogFuncMu       sync.RWMutex

	lastError   error
	lastErrorMu sync.Mutex
}

// New creates a Value with refresher function and starts periodic refresh loop
// Note that refreshFunc and Load() are not strongly synchronized by design.
// It means that you should not expect immediate synchronous value update after ticker ticks.
func New(refreshFunc func() (interface{}, error), tickerChan <-chan time.Time, slowLogFunc ...func(time.Duration)) *Value {
	var slf func(time.Duration)
	if len(slowLogFunc) > 0 {
		slf = slowLogFunc[0]
	}

	v := &Value{
		refreshFunc:         refreshFunc,
		valueNotificator:    make(chan bool, 1),
		slowLogFunc:         slf,
		slowLogFuncInterval: time.Second * 5,
	}
	v.store() // run refresh once before the ticker tics to set the value
	go v.loop(tickerChan)
	return v
}

// Load returns the value set at the most recent refreshFunc run
func (v *Value) Load() interface{} {
	return v.atomicValue.Load()
}

// Wait blocks until Value is refreshed, waiting up to <time.Duration>.
// Returns error if waited longer than Duration to update.
func (v *Value) Wait(ttl time.Duration) error {
	select {
	case <-v.valueNotificator:
		return nil
	case <-time.After(ttl):
		v.lastErrorMu.Lock()
		defer v.lastErrorMu.Unlock()
		if v.lastError != nil {
			return errors.Wrap(v.lastError, "last refresh returned an error")
		}
		return errors.New("Deadline exceeded")
	}
}

// loop calls refreshFunc on each tick
func (v *Value) loop(tickerChan <-chan time.Time) {
	for range tickerChan {
		v.store()
	}
}

// ProgressFunc reports long-running refreshFunc's progress.
// Runs every <interval> seconds while refreshFunc is running,
// passing elapsed time to the func.
func (v *Value) ProgressFunc(interval time.Duration, f func(time.Duration)) {
	v.slowLogFuncMu.Lock()
	defer v.slowLogFuncMu.Unlock()
	v.slowLogFunc = f
	v.slowLogFuncInterval = interval
}

// refresh runs refreshFunc once
func (v *Value) store() {

	retChan := make(chan bool, 1)

	// Debug routine that runs slow log func once it is set
	go func() {
		now := time.Now()

		// Set an interval, fallback to 5s if not set
		v.slowLogFuncMu.RLock()
		interval := v.slowLogFuncInterval
		v.slowLogFuncMu.RUnlock()

		for {
			select {
			case <-time.After(interval):
				v.slowLogFuncMu.RLock()
				if v.slowLogFunc != nil {
					v.slowLogFunc(time.Now().Sub(now))
					v.slowLogFuncMu.RLock()
					interval = v.slowLogFuncInterval
					v.slowLogFuncMu.RUnlock()
				}
				v.slowLogFuncMu.RUnlock()
			case <-retChan:
				return
			}
		}
	}()

	val, err := v.refreshFunc()
	retChan <- true
	close(retChan)

	if err != nil {
		// we're assuming refreshFunc does the logging
		v.lastErrorMu.Lock()
		defer v.lastErrorMu.Unlock()
		v.lastError = err
		return
	}

	v.atomicValue.Store(val)
	select {
	case v.valueNotificator <- true:
	default:
	}
}
