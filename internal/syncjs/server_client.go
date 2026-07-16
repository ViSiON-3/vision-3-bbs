package syncjs

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerServerClient creates stub server and client global objects.
// dorkit.js checks for these to detect 'sbbs' mode:
//
//	js.global.bbs !== undefined && js.global.server !== undefined
//	&& js.global.client !== undefined && js.global.user !== undefined
//	&& js.global.console !== undefined
func registerServerClient(vm *goja.Runtime, eng *Engine) {
	// server object — minimal stub
	server := vm.NewObject()
	jsutil.Set(server, "version", "ViSiON/3 SyncJS")
	jsutil.Set(server, "version_detail", "ViSiON/3 SyncJS Compatibility Layer")
	jsutil.Set(vm, "server", server)

	// client object — used by sbbs_console.js for connection info
	client := vm.NewObject()
	jsutil.Set(client, "protocol", "SSH")

	// client.socket with descriptor — sbbs_console.js reads client.socket.descriptor
	sock := vm.NewObject()
	jsutil.Set(sock, "descriptor", -1) // no real socket FD
	jsutil.Set(client, "socket", sock)

	jsutil.Set(client, "ip_address", "127.0.0.1")
	jsutil.Set(vm, "client", client)
}
