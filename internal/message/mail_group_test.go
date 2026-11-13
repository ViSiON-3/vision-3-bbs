package message

import "testing"

// A member's copy of group mail keeps the group and the letter's id, in a
// local area and in netmail; public posts and posts without an id don't
// carry them.
func TestMailGroupRoundTrip(t *testing.T) {
	for name, tc := range map[string]struct {
		mm   *MessageManager
		area int
		to   string
	}{
		"local private mail": {newReplyTestManager(t), 2, "Ann"},
		"netmail":            {newNetmailTestManager(t), 1, "Ann@21:1/100"},
	} {
		t.Run(name, func(t *testing.T) {
			n, err := tc.mm.Post(tc.area, "Caller", tc.to, "Meeting", "Tonight.", PostOptions{Private: true, SenderHandle: "Caller", MailGroup: "ops", MailGroupID: "abc123"})
			if err != nil {
				t.Fatal(err)
			}
			m, err := tc.mm.GetMessage(tc.area, n)
			if err != nil {
				t.Fatal(err)
			}
			if m.MailGroup != "ops" || m.MailGroupID != "abc123" || m.GroupAddress() != "@ops" {
				t.Errorf("group = %q %q %q", m.MailGroup, m.MailGroupID, m.GroupAddress())
			}
		})
	}
	mm := newReplyTestManager(t)
	for _, opts := range []PostOptions{
		{MailGroup: "ops", MailGroupID: "abc"},              // public
		{Private: true, MailGroup: "ops"},                   // no id
		{Private: true, MailGroup: " ", MailGroupID: "abc"}, // no name
	} {
		area := 2
		if !opts.Private {
			area = 1
		}
		n, err := mm.Post(area, "Caller", "Ann", "x", "y", opts)
		if err != nil {
			t.Fatal(err)
		}
		if m, _ := mm.GetMessage(area, n); m.MailGroup != "" || m.MailGroupID != "" {
			t.Errorf("%+v stored group %q %q", opts, m.MailGroup, m.MailGroupID)
		}
	}
}
