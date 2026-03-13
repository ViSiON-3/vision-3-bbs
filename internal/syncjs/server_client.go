package syncjs

import "github.com/dop251/goja"

// registerServerClient creates stub server and client global objects.
// dorkit.js checks for these to detect 'sbbs' mode:
//
//	js.global.bbs !== undefined && js.global.server !== undefined
//	&& js.global.client !== undefined && js.global.user !== undefined
//	&& js.global.console !== undefined
func registerServerClient(vm *goja.Runtime, eng *Engine) {
	// server object — minimal stub
	server := vm.NewObject()
	server.Set("version", "ViSiON/3 SyncJS")
	server.Set("version_detail", "ViSiON/3 SyncJS Compatibility Layer")
	vm.Set("server", server)

	// client object — used by sbbs_console.js for connection info
	client := vm.NewObject()
	client.Set("protocol", "SSH")

	// client.socket with descriptor — sbbs_console.js reads client.socket.descriptor
	sock := vm.NewObject()
	sock.Set("descriptor", -1) // no real socket FD
	client.Set("socket", sock)

	client.Set("ip_address", "127.0.0.1")
	vm.Set("client", client)
}
