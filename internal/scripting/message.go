package scripting

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// registerMessage creates the v3.message object for message area access.
func registerMessage(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	mgr := eng.providers.MessageMgr

	// areas() — list all message areas [{id, tag, name, description, type}].
	obj.Set("areas", func(call goja.FunctionCall) goja.Value {
		areas := mgr.ListAreas()
		arr := vm.NewArray()
		for i, a := range areas {
			arr.Set(itoa(i), messageAreaToJS(vm, a))
		}
		return arr
	})

	// area(tag) — get a single area by tag, returns object or null.
	obj.Set("area", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		tag := call.Arguments[0].String()
		a, found := mgr.GetAreaByTag(tag)
		if !found {
			return goja.Null()
		}
		return messageAreaToJS(vm, a)
	})

	// count(areaID) — message count in an area.
	obj.Set("count", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		areaID := int(call.Arguments[0].ToInteger())
		count, err := mgr.GetMessageCountForArea(areaID)
		if err != nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(count)
	})

	// get(areaID, msgNum) — get a message, returns object or null.
	obj.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Null()
		}
		areaID := int(call.Arguments[0].ToInteger())
		msgNum := int(call.Arguments[1].ToInteger())
		msg, err := mgr.GetMessage(areaID, msgNum)
		if err != nil {
			return goja.Null()
		}
		return displayMessageToJS(vm, msg)
	})

	// newCount(areaID) — count of unread messages for current user.
	obj.Set("newCount", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		areaID := int(call.Arguments[0].ToInteger())
		count, err := mgr.GetNewMessageCount(areaID, eng.session.UserHandle)
		if err != nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(count)
	})

	// post(areaID, {to, subject, body}) — post a message to an area.
	obj.Set("post", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errMissingArgs("post", "areaID, {to, subject, body}")))
		}
		areaID := int(call.Arguments[0].ToInteger())
		opts := call.Arguments[1].ToObject(vm)
		to := jsString(opts, "to", "All")
		subject := jsString(opts, "subject", "")
		body := jsString(opts, "body", "")
		replyTo := jsString(opts, "replyTo", "")

		msgNum, err := mgr.AddMessage(areaID, eng.session.UserHandle, to, subject, body, replyTo)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(msgNum)
	})

	// postPrivate(areaID, {to, subject, body}) — post a private message.
	obj.Set("postPrivate", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errMissingArgs("postPrivate", "areaID, {to, subject, body}")))
		}
		areaID := int(call.Arguments[0].ToInteger())
		opts := call.Arguments[1].ToObject(vm)
		to := jsString(opts, "to", "")
		if to == "" {
			panic(vm.NewGoError(fmt.Errorf("postPrivate: missing required 'to' recipient")))
		}
		subject := jsString(opts, "subject", "")
		body := jsString(opts, "body", "")
		replyTo := jsString(opts, "replyTo", "")

		msgNum, err := mgr.AddPrivateMessage(areaID, eng.session.UserHandle, to, subject, body, replyTo)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(msgNum)
	})

	// totalCount() — total messages across all areas.
	obj.Set("totalCount", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(mgr.GetTotalMessageCount())
	})

	v3.Set("message", obj)
}

func messageAreaToJS(vm *goja.Runtime, a *message.MessageArea) goja.Value {
	obj := vm.NewObject()
	obj.Set("id", a.ID)
	obj.Set("tag", a.Tag)
	obj.Set("name", a.Name)
	obj.Set("description", a.Description)
	obj.Set("type", a.AreaType)
	obj.Set("echoTag", a.EchoTag)
	obj.Set("conferenceID", a.ConferenceID)
	return obj
}

func displayMessageToJS(vm *goja.Runtime, msg *message.DisplayMessage) goja.Value {
	obj := vm.NewObject()
	obj.Set("msgNum", msg.MsgNum)
	obj.Set("from", msg.From)
	obj.Set("to", msg.To)
	obj.Set("subject", msg.Subject)
	obj.Set("body", msg.Body)
	obj.Set("date", msg.DateTime.Unix())
	obj.Set("msgID", msg.MsgID)
	obj.Set("replyID", msg.ReplyID)
	obj.Set("replyToNum", msg.ReplyToNum)
	obj.Set("isPrivate", msg.IsPrivate)
	obj.Set("areaID", msg.AreaID)
	return obj
}
