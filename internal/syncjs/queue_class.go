package syncjs

import (
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// registerQueueClass registers the Synchronet-compatible Queue constructor.
// Queue is an inter-thread message queue used by dorkit.js for I/O buffering.
// When a Queue's poll/read find no data, they fall back to reading from the
// session — this replaces Synchronet's background input thread model.
func registerQueueClass(vm *goja.Runtime, eng *Engine) {
	vm.Set("Queue", func(call goja.ConstructorCall) *goja.Object {
		q := &jsQueue{
			items: make([]goja.Value, 0),
			eng:   eng,
			vm:    vm,
		}

		obj := call.This
		obj.Set("write", func(fc goja.FunctionCall) goja.Value {
			val := goja.Undefined()
			if len(fc.Arguments) > 0 {
				val = fc.Arguments[0]
			}
			q.write(val)
			return vm.ToValue(true)
		})

		obj.Set("read", func(fc goja.FunctionCall) goja.Value {
			return q.read()
		})

		// poll(timeout_ms) — returns true if data available, false on timeout.
		obj.Set("poll", func(fc goja.FunctionCall) goja.Value {
			timeout := int64(0)
			if len(fc.Arguments) > 0 {
				timeout = fc.Arguments[0].ToInteger()
			}
			return vm.ToValue(q.poll(time.Duration(timeout) * time.Millisecond))
		})

		obj.Set("peek", func(fc goja.FunctionCall) goja.Value {
			q.mu.Lock()
			defer q.mu.Unlock()
			if len(q.items) == 0 {
				return goja.Undefined()
			}
			return q.items[0]
		})

		obj.DefineAccessorProperty("data_waiting", vm.ToValue(func(fc goja.FunctionCall) goja.Value {
			q.mu.Lock()
			defer q.mu.Unlock()
			return vm.ToValue(len(q.items) > 0)
		}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

		return nil
	})
}

// jsQueue is the Go backing for a Synchronet Queue object.
type jsQueue struct {
	mu     sync.Mutex
	items  []goja.Value
	notify chan struct{}
	eng    *Engine
	vm     *goja.Runtime
}

func (q *jsQueue) write(val goja.Value) {
	q.mu.Lock()
	q.items = append(q.items, val)
	ch := q.notify
	q.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (q *jsQueue) read() goja.Value {
	q.mu.Lock()
	if len(q.items) > 0 {
		val := q.items[0]
		q.items = q.items[1:]
		q.mu.Unlock()
		return val
	}
	q.mu.Unlock()

	// Return synthetic cursor position response if pending.
	// DORKit's getkey() expects "POSITION_row_col\x00..." from the queue,
	// which it parses to set terminal dimensions via detect_ansi().
	if q.eng.pendingDSR {
		q.eng.pendingDSR = false
		rows := q.eng.session.ScreenHeight
		cols := q.eng.session.ScreenWidth
		return q.vm.ToValue(fmt.Sprintf("POSITION_%d_%d\x00\x1b[%d;%dR", rows, cols, rows, cols))
	}

	// Fall back to session I/O — replaces Synchronet's background input thread
	key, err := q.eng.readKey(0)
	if err != nil || key == "" {
		return goja.Undefined()
	}
	return q.vm.ToValue(key)
}

func (q *jsQueue) poll(timeout time.Duration) bool {
	q.mu.Lock()
	if len(q.items) > 0 {
		q.mu.Unlock()
		return true
	}
	q.mu.Unlock()

	// Pending cursor position response counts as available data
	if q.eng.pendingDSR {
		return true
	}

	if timeout <= 0 {
		// Non-blocking: just check if session has data via a very short read
		timeout = time.Millisecond
	}

	// Fall back to session I/O with timeout
	key, err := q.eng.readKey(timeout)
	if err != nil || key == "" {
		return false
	}
	// Got a key — buffer it in the queue for the next read()
	q.mu.Lock()
	q.items = append(q.items, q.vm.ToValue(key))
	q.mu.Unlock()
	return true
}
