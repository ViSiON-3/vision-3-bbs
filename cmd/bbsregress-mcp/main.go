// bbsregress-mcp exposes one local ViSiON/3 instance to MCP for interactive regression work.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/ViSiON-3/vision-3-bbs/internal/bbsregression"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	host := flag.String("host", "127.0.0.1", "test BBS host")
	port := flag.Int("port", 2323, "test BBS TCP port")
	protocol := flag.String("protocol", "telnet", "telnet or ssh")
	sshUser := flag.String("ssh-user", "", "SSH username; required for SSH")
	sshKey := flag.String("ssh-host-key-sha256", "", "pinned SSH SHA256 host key fingerprint")
	term := flag.String("term", "ANSI", "terminal type reported to the BBS")
	encoding := flag.String("encoding", "cp437", "cp437 or utf8")
	cols := flag.Int("cols", 80, "terminal columns")
	rows := flag.Int("rows", 25, "terminal rows")
	flag.Parse()

	srv, err := bbsregression.NewServer(bbsregression.Profile{
		Host: *host, Port: *port, Protocol: *protocol, SSHUser: *sshUser,
		SSHHostKeySHA256: *sshKey, Terminal: *term, Encoding: *encoding,
		Columns: *cols, Rows: *rows,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
