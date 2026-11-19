package scripting

import (
	"fmt"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerNodes creates the v3.nodes object for inter-node access.
func registerNodes(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	registry := eng.providers.SessionRegistry

	// list() — returns array of active nodes [{node, handle, activity, idle, invisible}].
	// Invisible sessions are excluded unless the current user is sysop (accessLevel >= 200),
	// and test logins unless the current user is at the board's sysop level.
	jsutil.Set(obj, "list", func(call goja.FunctionCall) goja.Value {
		sessions := registry.ListActive()
		arr := vm.NewArray()
		isSysop := eng.session.AccessLevel >= 200
		i := 0
		for _, s := range sessions {
			s.Mutex.RLock()
			invisible, testLogin := s.Invisible, s.TestLogin
			handle := ""
			if s.User != nil {
				handle = s.User.Handle
			}
			activity := s.Activity
			nodeID := s.NodeID
			idle := time.Since(s.LastActivity).Seconds()
			s.Mutex.RUnlock()

			if hiddenNode(invisible, testLogin, isSysop, eng.session.SysOp) {
				continue
			}

			entry := vm.NewObject()
			jsutil.Set(entry, "node", nodeID)
			jsutil.Set(entry, "handle", handle)
			jsutil.Set(entry, "activity", activity)
			jsutil.Set(entry, "idle", int(idle))
			jsutil.Set(entry, "invisible", invisible)
			jsutil.Set(arr, itoa(i), entry)
			i++
		}
		return arr
	})

	// count() — returns the number of active nodes, leaving out those list() leaves out.
	jsutil.Set(obj, "count", func(call goja.FunctionCall) goja.Value {
		sessions := registry.ListActive()
		isSysop := eng.session.AccessLevel >= 200
		count := 0
		for _, s := range sessions {
			s.Mutex.RLock()
			invisible, testLogin := s.Invisible, s.TestLogin
			s.Mutex.RUnlock()
			if hiddenNode(invisible, testLogin, isSysop, eng.session.SysOp) {
				continue
			}
			count++
		}
		return vm.ToValue(count)
	})

	// send(nodeNum, message) — send a page message to a specific node.
	// Returns true if the node exists and the message was queued.
	jsutil.Set(obj, "send", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errMissingArgs("send", "nodeNum, message")))
		}
		nodeNum := int(call.Arguments[0].ToInteger())
		msg := call.Arguments[1].String()
		if err := checkChars("v3.nodes.send", "the page", msg, maxPageChars); err != nil {
			panic(vm.NewGoError(err))
		}

		target := registry.Get(nodeNum)
		if target == nil {
			return vm.ToValue(false)
		}

		formatted := fmt.Sprintf("[Node %d - %s] %s", eng.session.NodeNumber, eng.session.UserHandle, msg)
		target.AddPage(formatted)
		return vm.ToValue(true)
	})

	// broadcast(message) — send a page message to all active nodes (except self).
	jsutil.Set(obj, "broadcast", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		msg := call.Arguments[0].String()
		if err := checkChars("v3.nodes.broadcast", "the page", msg, maxPageChars); err != nil {
			panic(vm.NewGoError(err))
		}
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

	jsutil.Set(v3, "nodes", obj)
}

// hiddenNode reports whether a node is left out of v3.nodes for a viewer:
// a test login unless the viewer is at the board's sysop level (sysOp),
// any other invisible session unless the viewer is at level 200 or more
// (level200).
func hiddenNode(invisible, testLogin, level200, sysOp bool) bool {
	if testLogin {
		return !sysOp
	}
	return invisible && !level200
}
