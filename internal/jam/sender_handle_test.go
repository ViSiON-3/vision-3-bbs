package jam

import "testing"

// TestSenderHandleRoundTrip checks that a local write stores SenderHandle
// and reads it back, through both write paths, and that a received
// message cannot carry one.
func TestSenderHandleRoundTrip(t *testing.T) {
	b := openTestBase(t)

	local := NewMessage()
	local.From, local.To, local.Subject, local.Text = "Pat Example", "All", "Hi", "Hello"
	local.SenderHandle = "pat"
	n1, err := b.WriteMessage(local)
	if err != nil {
		t.Fatal(err)
	}

	net := NewMessage()
	net.From, net.To, net.Subject, net.Text = "Pat Example", "Bob", "Hi", "Hello"
	net.OrigAddr, net.DestAddr = "1:2/3", "1:2/4"
	net.SenderHandle = "pat"
	n2, err := b.WriteMessageExt(net, MsgTypeNetmailMsg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	in := NewMessage()
	in.From, in.To, in.Subject, in.Text = "Bob", "Pat Example", "Re: Hi", "Hello"
	in.OrigAddr = "1:2/4"
	in.SenderHandle = "pat"
	n3, err := b.WriteReceivedMessage(in, MsgTypeNetmailMsg)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		desc string
		n    int
		want string
	}{
		{"local message", n1, "pat"},
		{"local netmail", n2, "pat"},
		{"received netmail", n3, ""},
	} {
		msg, err := b.ReadMessage(tt.n)
		if err != nil {
			t.Fatalf("%s: ReadMessage: %v", tt.desc, err)
		}
		if msg.SenderHandle != tt.want {
			t.Errorf("%s: SenderHandle = %q, want %q", tt.desc, msg.SenderHandle, tt.want)
		}
	}
}
