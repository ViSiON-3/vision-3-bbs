package scripting

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// registerNodes creates the v3.nodes object for inter-node access.
func registerNodes(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	registry := eng.providers.SessionRegistry

	// list() — returns array of active nodes [{node, handle, activity, idle, invisible}].
	// Invisible sessions are excluded unless the current user is sysop (accessLevel >= 200).
	obj.Set("list", func(call goja.FunctionCall) goja.Value {
		sessions := registry.ListActive()
		arr := vm.NewArray()
		isSysop := eng.session.AccessLevel >= 200
		i := 0
		for _, s := range sessions {
			s.Mutex.RLock()
			invisible := s.Invisible
			handle := ""
			if s.User != nil {
				handle = s.User.Handle
			}
			activity := s.Activity
			nodeID := s.NodeID
			idle := time.Since(s.LastActivity).Seconds()
			s.Mutex.RUnlock()

			if invisible && !isSysop {
				continue
			}

			entry := vm.NewObject()
			entry.Set("node", nodeID)
			entry.Set("handle", handle)
			entry.Set("activity", activity)
			entry.Set("idle", int(idle))
			entry.Set("invisible", invisible)
			arr.Set(itoa(i), entry)
			i++
		}
		return arr
	})

	// count() — returns the number of active nodes (respects invisible flag).
	obj.Set("count", func(call goja.FunctionCall) goja.Value {
		sessions := registry.ListActive()
		isSysop := eng.session.AccessLevel >= 200
		count := 0
		for _, s := range sessions {
			s.Mutex.RLock()
			invisible := s.Invisible
			s.Mutex.RUnlock()
			if !invisible || isSysop {
				count++
			}
		}
		return vm.ToValue(count)
	})

	// send(nodeNum, message) — send a page message to a specific node.
	// Returns true if the node exists and the message was queued.
	obj.Set("send", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errMissingArgs("send", "nodeNum, message")))
		}
		nodeNum := int(call.Arguments[0].ToInteger())
		msg := call.Arguments[1].String()

		target := registry.Get(nodeNum)
		if target == nil {
			return vm.ToValue(false)
		}

		formatted := fmt.Sprintf("[Node %d - %s] %s", eng.session.NodeNumber, eng.session.UserHandle, msg)
		target.AddPage(formatted)
		return vm.ToValue(true)
	})

	// broadcast(message) — send a page message to all active nodes (except self).
	obj.Set("broadcast", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		msg := call.Arguments[0].String()
		formatted := fmt.Sprintf("[Node %d - %s] %s", eng.session.NodeNumber, eng.session.UserHandle, msg)

		sessions := registry.ListActive()
		for _, s := range sessions {
			if s.NodeID == eng.session.NodeNumber {
				continue
			}
			s.AddPage(formatted)
		}
		return goja.Undefined()
	})

	v3.Set("nodes", obj)
}
