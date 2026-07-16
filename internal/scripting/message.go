package scripting

import (
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/dop251/goja"
)

// registerMessage creates the v3.message object for message area access.
func registerMessage(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	mgr := eng.providers.MessageMgr

	// areas() — list all message areas [{id, tag, name, description, type}].
	jsutil.Set(obj, "areas", func(call goja.FunctionCall) goja.Value {
		areas := mgr.ListAreas()
		arr := vm.NewArray()
		for i, a := range areas {
			jsutil.Set(arr, itoa(i), messageAreaToJS(vm, a))
		}
		return arr
	})

	// area(tag) — get a single area by tag, returns object or null.
	jsutil.Set(obj, "area", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "count", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "get", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "newCount", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "post", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "postPrivate", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "totalCount", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(mgr.GetTotalMessageCount())
	})

	jsutil.Set(v3, "message", obj)
}

func messageAreaToJS(vm *goja.Runtime, a *message.MessageArea) goja.Value {
	obj := vm.NewObject()
	jsutil.Set(obj, "id", a.ID)
	jsutil.Set(obj, "tag", a.Tag)
	jsutil.Set(obj, "name", a.Name)
	jsutil.Set(obj, "description", a.Description)
	jsutil.Set(obj, "type", a.AreaType)
	jsutil.Set(obj, "echoTag", a.EchoTag)
	jsutil.Set(obj, "conferenceID", a.ConferenceID)
	return obj
}

func displayMessageToJS(vm *goja.Runtime, msg *message.DisplayMessage) goja.Value {
	obj := vm.NewObject()
	jsutil.Set(obj, "msgNum", msg.MsgNum)
	jsutil.Set(obj, "from", msg.From)
	jsutil.Set(obj, "to", msg.To)
	jsutil.Set(obj, "subject", msg.Subject)
	jsutil.Set(obj, "body", msg.Body)
	jsutil.Set(obj, "date", msg.DateTime.Unix())
	jsutil.Set(obj, "msgID", msg.MsgID)
	jsutil.Set(obj, "replyID", msg.ReplyID)
	jsutil.Set(obj, "replyToNum", msg.ReplyToNum)
	jsutil.Set(obj, "isPrivate", msg.IsPrivate)
	jsutil.Set(obj, "areaID", msg.AreaID)
	return obj
}
