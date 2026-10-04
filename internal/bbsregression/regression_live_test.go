//go:build bbsregression

package bbsregression

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const regressionMainMenu = `(?i)\[TL:[^\]]+\]\s*->`

// TestLocalTerminalRegressionSuite checks terminal journeys against a
// disposable, seeded ViSiON/3 instance. Login counters and the chosen message
// header style may be persisted while exercising the rendered BBS UI.
func TestLocalTerminalRegressionSuite(t *testing.T) {
	board := openRegressionBoard(t)
	board.login(t, true)
	board.wait(t, regressionMainMenu, 8)

	journeys := []struct {
		name string
		keys string
		want string
		menu string
	}{
		{name: "user_stats", keys: "Y", want: `(?i)Access Level: 255`, menu: regressionMainMenu},
		{name: "system_stats", keys: "S", want: `(?i)System Statistics`, menu: regressionMainMenu},
		{name: "version_info", keys: "^", want: `(?i)Go Edition`, menu: regressionMainMenu},
		{name: "user_directory", keys: "L", want: `(?i)Pending validation:`, menu: regressionMainMenu},
		{name: "news", keys: "N", want: regressionMainMenu, menu: regressionMainMenu},
		{name: "last_callers", keys: "W", want: `(?i)User Name`, menu: regressionMainMenu},
		{name: "who_is_online", keys: "/W", want: `(?i)Activity`, menu: regressionMainMenu},
	}
	for _, journey := range journeys {
		if !t.Run(journey.name, func(t *testing.T) {
			board.wait(t, journey.menu, 8)
			board.sendExpect(t, journey.keys+"{enter}", journey.want, 8)
			board.sendExpect(t, "{enter}", journey.menu, 8)
		}) {
			return
		}
	}
	if !t.Run("user_settings", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		screen := board.sendExpect(t, "K{enter}", `(?i)\[A\] Screen Width`, 8)
		for _, label := range []string{"[E] Header Style", "[F] Auto-Signature", "[G] Real Name", "[L] File Columns"} {
			if !strings.Contains(screen.Text, label) {
				t.Errorf("user settings screen is missing %q:\n%s", label, screen.Text)
			}
		}
		// Open and cancel one typed field and the header-style picker. These
		// exercise actual nested UI actions without changing the account.
		board.sendExpect(t, "A", `(?i)Screen Width:`, 8)
		board.sendExpect(t, "{esc}", `(?i)\[A\] Screen Width`, 8)
		board.sendExpect(t, "E", `(?i)Available Message Headers`, 8)
		board.sendExpect(t, "Q", `(?i)\[E\] Header Style`, 8)
		// Other settings can persist immediately, so leave their fields alone.
		board.sendExpect(t, "q", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("autosig_settings_cancel", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		screen := board.sendExpect(t, "U{enter}", `(?i)Auto-Signature`, 8)
		if !strings.Contains(strings.ToLower(screen.Text), "do not have an auto-signature") {
			t.Errorf("empty auto-signature state is missing its message:\n%s", screen.Text)
		}
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("oneliner_readonly", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		screen := board.sendExpect(t, "O{enter}", `(?i)No one-liners yet`, 8)
		if !strings.Contains(strings.ToLower(screen.Text), "add a one liner") {
			t.Errorf("empty oneliner screen is missing the add prompt:\n%s", screen.Text)
		}
		board.sendExpect(t, "n", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("page_prompt_cancel", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		screen := board.sendExpect(t, "P{enter}", `(?i)Page which node`, 8)
		if !strings.Contains(strings.ToLower(screen.Text), "online nodes") {
			t.Errorf("page prompt is missing the online-node heading:\n%s", screen.Text)
		}
		board.sendExpect(t, "{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("main_vote_decline_create", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "V{enter}", `(?i)Create first topic`, 8)
		board.sendExpect(t, "N{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("main_chat_join_and_quit", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		screen := board.sendExpect(t, "C{enter}", `(?i)Joined #lobby`, 8)
		if !strings.Contains(screen.Text, "/rooms /join") || !strings.Contains(screen.Text, "Users: Felonius") {
			t.Errorf("local chat screen is missing its help or user status:\n%s", screen.Text)
		}
		board.sendExpect(t, "/q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("message_menu_and_areas", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "M{enter}", `(?i)Message Menu`, 8)
		board.sendExpect(t, "*{enter}", `(?i)(Local Areas|General Discussion)`, 8)
		board.sendExpect(t, "{enter}", `(?i)Message Menu`, 8)
	}) {
		return
	}
	if !t.Run("message_area_picker_cancel", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		board.sendExpect(t, "A{enter}", `(?i)General Discussion`, 8)
		board.sendExpect(t, "Q", `(?i)Message Menu`, 8)
	}) {
		return
	}
	if !t.Run("message_conference_picker_cancel", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		screen := board.sendExpect(t, "C{enter}", `(?i)Current Conference: Local Areas`, 8)
		for _, name := range []string{"Local", "FelonyNet"} {
			if !strings.Contains(strings.ToLower(screen.Text), strings.ToLower(name)) {
				t.Errorf("conference picker is missing %q:\n%s", name, screen.Text)
			}
		}
		board.sendExpect(t, "Q", `(?i)Message Menu`, 8)
	}) {
		return
	}
	if !t.Run("message_conference_next_previous", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		board.sendExpect(t, "}{enter}", `(?i)FelonyNet`, 8)
		screen := board.sendExpect(t, "{{{enter}", `(?i)Message Menu`, 8)
		if !strings.Contains(strings.ToLower(screen.Text), "[area] local >") {
			t.Errorf("previous conference did not restore the Local message area:\n%s", screen.Text)
		}
	}) {
		return
	}
	if !t.Run("compose_message_blank_title_abort", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		board.sendExpect(t, "P{enter}", `(?i)Title`, 8)
		board.sendExpect(t, "{enter}", `(?i)Message Menu`, 8)
	}) {
		return
	}
	if !t.Run("message_empty_list_and_autosig", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		board.sendExpect(t, "L{enter}", `(?i)Message Menu`, 8)
		board.wait(t, `(?i)Message Menu`, 8)
		screen := board.sendExpect(t, "S{enter}", `(?i)Auto-Signature`, 8)
		if !strings.Contains(strings.ToLower(screen.Text), "do not have an auto-signature") {
			t.Errorf("empty auto-signature state is missing its message:\n%s", screen.Text)
		}
		board.sendExpect(t, "Q{enter}", `(?i)Message Menu`, 8)
	}) {
		return
	}
	if !t.Run("message_area_navigation", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		before := regressionCurrentMessageArea(t, board.snapshot(t).Text)
		next := "Private Mail"
		if strings.EqualFold(before, "Private Mail") {
			next = "General Discussion"
		}
		board.sendExpect(t, "]{enter}", `(?i)`+regexp.QuoteMeta(next), 8)
		board.sendExpect(t, "[{enter}", `(?i)`+regexp.QuoteMeta(before), 8)
	}) {
		return
	}

	if !t.Run("message_header_picker_and_empty_reader", func(t *testing.T) {
		board.wait(t, `(?i)Message Menu`, 8)
		board.sendExpect(t, "H{enter}", `(?i)Available Message Headers`, 8)
		board.sendExpect(t, " ", `(?i)Message Menu`, 8)
		board.sendExpect(t, "R{enter}", `(?i)Message Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("qwk_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "M{enter}", `(?i)Message Menu`, 8)
		board.sendExpect(t, "W{enter}", `(?i)QWK Mail`, 8)
		board.sendExpect(t, "Q{enter}", `(?i)Message Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("private_mail_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "E{enter}", `(?i)Email Menu`, 8)
		board.sendExpect(t, "S{enter}", `(?i)Send private mail to:`, 8)
		board.sendExpect(t, "{esc}", `(?i)Abort Post\?`, 8)
		board.send(t, "Y")
		board.sendExpect(t, "R{enter}", `(?i)No private mail found`, 8)
		board.sendExpect(t, "L{enter}", `(?i)No messages in this area`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("file_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "T{enter}", `(?i)Transfer Menu`, 8)
		board.sendExpect(t, "A{enter}", `(?i)General Files`, 8)
		board.sendExpect(t, "{esc}", `(?i)Transfer Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("file_actions_empty_states", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		fileMenu := `(?i)Transfer Menu`
		board.sendExpect(t, "T{enter}", fileMenu, 8)

		board.sendExpect(t, "D{enter}", `(?i)Add to .*Batch`, 8)
		board.sendExpect(t, "{enter}", fileMenu, 8)

		board.sendExpect(t, "V{enter}", `(?i)filename to view`, 8)
		board.sendExpect(t, "{enter}", fileMenu, 8)

		board.sendExpect(t, "I{enter}", `(?i)filename to view info`, 8)
		board.sendExpect(t, "{enter}", fileMenu, 8)

		board.sendExpect(t, "S{enter}", `(?i)Enter search text`, 8)
		board.sendExpect(t, "bbs-regression-no-such-file-4729{enter}", fileMenu, 8)

		board.sendExpect(t, "N{enter}", `(?i)No new files found since your last login`, 8)
		board.sendExpect(t, "{enter}", fileMenu, 8)

		board.sendExpect(t, "B{enter}", `(?i)No files are tagged for download`, 8)
		board.wait(t, fileMenu, 8)

		board.sendExpect(t, "-{enter}", `(?i)Your batch queue is empty`, 8)
		board.wait(t, fileMenu, 8)

		board.sendExpect(t, "K{enter}", `(?i)File Listing Columns`, 8)
		board.sendExpect(t, "Q{enter}", fileMenu, 8)

		board.sendExpect(t, "Z{enter}", `(?i)General Files`, 8)
		board.sendExpect(t, "Q", fileMenu, 8)

		board.sendExpect(t, "U{enter}", `(?i)Transfer Protocols`, 8)
		board.sendExpect(t, "Q{enter}", fileMenu, 8)

		board.sendExpect(t, "W{enter}", `(?i)No files in this area`, 8)
		board.sendExpect(t, "Q", fileMenu, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("door_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "D{enter}", `(?i)Doors`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("door_actions", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "D{enter}", `(?i)Doors`, 8)
		doorMenu := `(?i)TL:[^\]]+\]\s*Doors\s*->`

		board.sendExpect(t, "H{enter}", `(?i)Would you like to leave a message`, 8)
		board.sendExpect(t, "n", `(?i)Thanks for trying VPL scripting`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)

		board.sendExpect(t, "O{enter}", `(?i)Add an oneliner`, 8)
		board.sendExpect(t, "n", `(?i)\[Press any key\]`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)

		board.sendExpect(t, "V{enter}", `(?i)Your choice \(1-4\)`, 8)
		board.sendExpect(t, "{enter}", `(?i)Total votes: 0`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)

		board.sendExpect(t, "Y{enter}", `(?i)System Statistics`, 8)
		board.sendExpect(t, "{enter}", `(?i)\[Press any key\]`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)

		board.sendExpect(t, "A{enter}", `(?i)Leave a new auto-message`, 8)
		board.sendExpect(t, "n", `(?i)\[Press any key\]`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)

		board.sendExpect(t, "U{enter}", `(?i)Top Callers`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)

		board.sendExpect(t, "T{enter}", `(?i)Last 10 Callers`, 8)
		board.sendExpect(t, "{enter}", doorMenu, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("rumor_listing", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "R{enter}", `(?i)Rumors`, 8)
		board.sendExpect(t, "L{enter}", `(?i)Rumors Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("rumor_add_cancel", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "R{enter}", `(?i)Rumors`, 8)
		board.sendExpect(t, "A{enter}", `(?i)Anonymous`, 8)
		board.sendExpect(t, "N", `(?i)Minimum security level|required to view`, 8)
		board.sendExpect(t, "{enter}", `(?i)Enter Rumor`, 8)
		board.sendExpect(t, "{enter}", `(?i)Rumors Menu`, 8)
		board.sendExpect(t, "L{enter}", `(?i)Rumors Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("rumor_empty_actions", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "R{enter}", `(?i)Rumors`, 8)
		rumorsMenu := `(?i)Rumors Menu`

		board.sendExpect(t, "N{enter}", `(?i)No new rumors since your last login`, 8)
		board.sendExpect(t, "{enter}", rumorsMenu, 8)

		board.sendExpect(t, "S{enter}", rumorsMenu, 8)
		board.wait(t, rumorsMenu, 8)
		board.sendExpect(t, "D{enter}", rumorsMenu, 8)
		board.wait(t, rumorsMenu, 8)
		board.sendExpect(t, "*{enter}", rumorsMenu, 8)
		board.wait(t, rumorsMenu, 8)

		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("bbs_directory", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "B{enter}", `(?i)BBS List`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("bbs_directory_actions", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "B{enter}", `(?i)BBS List`, 8)
		bbsMenu := `(?i)\[BBS List\] Command ->`

		board.sendExpect(t, "L{enter}", `(?i)No BBS listings yet`, 8)
		board.sendExpect(t, "{enter}", bbsMenu, 8)

		board.sendExpect(t, "A{enter}", `(?i)BBS Name`, 8)
		board.sendExpect(t, "{enter}", `(?i)\[BBS List\] Command ->`, 8)

		board.sendExpect(t, "C{enter}", bbsMenu, 8)
		board.wait(t, bbsMenu, 8)
		board.sendExpect(t, "D{enter}", bbsMenu, 8)
		board.wait(t, bbsMenu, 8)
		board.sendExpect(t, "V{enter}", bbsMenu, 8)
		board.wait(t, bbsMenu, 8)
		board.sendExpect(t, "L{enter}", `(?i)No BBS listings yet`, 8)
		board.sendExpect(t, "{enter}", bbsMenu, 8)

		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("infoforms_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "I{enter}", `(?i)InfoForms`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("sysop_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "X{enter}", `(?i)Admin Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("sysop_readonly_actions", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		adminMenu := `(?i)Admin\s+Menu\s*[-|>]`
		board.sendExpect(t, "X{enter}", adminMenu, 8)

		// The seeded sysop is validated and purge retention is disabled. These
		// routes exercise the live admin UI without editing account or board data.
		board.sendExpect(t, "V{enter}", `(?i)No users pending validation`, 8)
		board.sendExpect(t, "{enter}", adminMenu, 8)

		board.sendExpect(t, "E{enter}", `(?i)User Editor`, 8)
		board.sendExpect(t, "q", adminMenu, 8)

		board.sendExpect(t, "P{enter}", `(?i)User purge is disabled`, 8)
		board.sendExpect(t, "{enter}", adminMenu, 8)

		board.sendExpect(t, "W{enter}", `(?i)System News Management`, 8)
		board.sendExpect(t, "L{enter}", `(?i)Title\s+Min\s+Max\s+Display`, 8)
		board.sendExpect(t, "{enter}", `(?i)System News Management`, 8)
		board.sendExpect(t, "Q{enter}", adminMenu, 8)

		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("sysop_toggle_new_users_and_restore", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "X{enter}", `(?i)Admin\s+Menu`, 8)
		board.sendExpect(t, "N{enter}", `(?i)New user registrations: CLOSED`, 8)
		board.sendExpect(t, "N{enter}", `(?i)New user registrations: OPEN`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("sysop_voting_decline_first_topic", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "X{enter}", `(?i)Admin\s+Menu`, 8)
		board.sendExpect(t, "T{enter}", `(?i)Create first topic`, 8)
		board.sendExpect(t, "N{enter}", `(?i)Admin\s+Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("sysop_command_aliases", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "/SYSOP{enter}", `(?i)Admin Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
		board.sendExpect(t, "%{enter}", `(?i)Admin Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("v3net_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "X{enter}", `(?i)Admin Menu`, 8)
		board.sendExpect(t, "3{enter}", `(?i)V3Net`, 8)
		board.sendExpect(t, "Q{enter}", `(?i)Admin Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}
	if !t.Run("file_to_message_menu", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "T{enter}", `(?i)Transfer Menu`, 8)
		board.sendExpect(t, "M{enter}", `(?i)Message Menu`, 8)
		board.sendExpect(t, "Q{enter}", regressionMainMenu, 8)
	}) {
		return
	}

	if !t.Run("logoff_prompt", func(t *testing.T) {
		board.wait(t, regressionMainMenu, 8)
		board.sendExpect(t, "G{enter}", `(?i)(Log off now|log off|logoff|goodbye|are you sure)`, 8)
		// Cancel the prompt so each check remains a single logged-in journey.
		board.sendExpect(t, "N", regressionMainMenu, 8)
	}) {
		return
	}
}

// TestLocalFastLoginChoices checks all three post-login destinations. Each
// journey logs in on a separate disposable session so its menu choice cannot
// affect the following checks.
func TestLocalFastLoginChoices(t *testing.T) {
	for _, journey := range []struct {
		name string
		key  string
		menu string
		next string
	}{
		{name: "fast_login_main", key: "2", menu: regressionMainMenu},
		{name: "fast_login_file_menu", key: "3", menu: `(?i)Transfer Menu`, next: "Q"},
		{name: "fast_login_message_menu", key: "4", menu: `(?i)Message Menu`, next: "Q"},
	} {
		if !t.Run(journey.name, func(t *testing.T) {
			board := openRegressionBoard(t)
			board.loginToFastMenu(t)
			board.sendExpect(t, journey.key+"{enter}", journey.menu, 8)
			if journey.next != "" {
				board.sendExpect(t, journey.next+"{enter}", regressionMainMenu, 8)
			}
		}) {
			return
		}
	}
}

func regressionCurrentMessageArea(t *testing.T, screen string) string {
	t.Helper()
	area := regexp.MustCompile(`(?mi)\[Area\]\s+[^>\r\n]+>\s*([^\r\n]+)`).FindStringSubmatch(screen)
	if len(area) != 2 {
		t.Fatalf("current message area missing from terminal screen:\n%s", screen)
	}
	return strings.TrimSpace(area[1])
}

// TestLocalRejectedPasswordRegression checks the visible authentication error
// and confirms that a failed password does not reach the logged-in menu.
func TestLocalRejectedPasswordRegression(t *testing.T) {
	if os.Getenv("BBS_REGRESSION_TEST_BAD_PASSWORD") != "1" {
		t.Skip("set BBS_REGRESSION_TEST_BAD_PASSWORD=1 to run the lockout-counter test")
	}
	board := openRegressionBoard(t)
	board.login(t, false)
	board.send(t, "not-the-right-password{enter}")
	board.wait(t, `(?i)Login incorrect`, 8)
	if regexp.MustCompile(regressionMainMenu).MatchString(board.snapshot(t).Text) {
		t.Fatal("wrong password reached the main menu")
	}
}

// TestLocalSSHLoginRegression exercises the same login journey over SSH when
// an isolated SSH endpoint and pinned host-key fingerprint are provided.
func TestLocalSSHLoginRegression(t *testing.T) {
	host := os.Getenv("BBS_REGRESSION_SSH_HOST")
	portText := os.Getenv("BBS_REGRESSION_SSH_PORT")
	sshUser := os.Getenv("BBS_REGRESSION_SSH_USER")
	fingerprint := os.Getenv("BBS_REGRESSION_SSH_HOST_KEY_SHA256")
	if host == "" || portText == "" || sshUser == "" || fingerprint == "" || os.Getenv("BBS_REGRESSION_HANDLE") == "" || os.Getenv("BBS_REGRESSION_PASSWORD") == "" {
		t.Skip("set BBS_REGRESSION_SSH_HOST, BBS_REGRESSION_SSH_PORT, BBS_REGRESSION_SSH_USER, BBS_REGRESSION_SSH_HOST_KEY_SHA256, BBS_REGRESSION_HANDLE, and BBS_REGRESSION_PASSWORD for live SSH tests")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("BBS_REGRESSION_SSH_PORT: %v", err)
	}
	board := openRegressionBoardWithProfile(t, Profile{
		Host: host, Port: port, Protocol: "ssh", SSHUser: sshUser,
		SSHHostKeySHA256: fingerprint, Terminal: "ANSI", Encoding: "cp437", Columns: 80, Rows: 25,
	})
	board.login(t, true)
}

type regressionBoard struct {
	client  *mcp.ClientSession
	session string
}

func openRegressionBoard(t *testing.T) *regressionBoard {
	t.Helper()
	host := os.Getenv("BBS_REGRESSION_HOST")
	portText := os.Getenv("BBS_REGRESSION_PORT")
	handle := os.Getenv("BBS_REGRESSION_HANDLE")
	password := os.Getenv("BBS_REGRESSION_PASSWORD")
	if host == "" || portText == "" || handle == "" || password == "" {
		t.Skip("set BBS_REGRESSION_HOST, BBS_REGRESSION_PORT, BBS_REGRESSION_HANDLE, and BBS_REGRESSION_PASSWORD for live-board tests")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("BBS_REGRESSION_PORT: %v", err)
	}
	return openRegressionBoardWithProfile(t, Profile{Host: host, Port: port, Protocol: "telnet", Terminal: "ANSI", Encoding: "cp437", Columns: 80, Rows: 25})
}

func openRegressionBoardWithProfile(t *testing.T, profile Profile) *regressionBoard {
	t.Helper()
	server, err := NewServer(profile)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "bbs-live-regression"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	board := &regressionBoard{client: client}
	t.Cleanup(func() {
		if board.session != "" {
			_, _ = board.client.CallTool(context.Background(), &mcp.CallToolParams{Name: "disconnect", Arguments: map[string]any{"session": board.session}})
		}
		_ = client.Close()
	})
	first := board.call(t, "connect", map[string]any{})
	if first.Session == "" || !first.Connected {
		t.Fatalf("connect failed: %+v", first)
	}
	board.session = first.Session
	return board
}

func (b *regressionBoard) login(t *testing.T, succeed bool) {
	t.Helper()
	first := b.loginThroughPassword(t, succeed)
	if !succeed {
		// Leave the caller at the password prompt for the negative case.
		return
	}
	if strings.Contains(strings.ToLower(first.Text), "fast login") {
		first = b.send(t, "2{enter}")
	}
	first = b.wait(t, regressionMainMenu, 30)
	if first.Matched == nil || !*first.Matched {
		t.Fatalf("main menu not reached after login:\n%s", first.Text)
	}
}

func (b *regressionBoard) loginToFastMenu(t *testing.T) {
	t.Helper()
	first := b.loginThroughPassword(t, true)
	if !strings.Contains(strings.ToLower(first.Text), "fast login") {
		t.Fatalf("fast login menu not reached after authentication:\n%s", first.Text)
	}
}

func (b *regressionBoard) loginThroughPassword(t *testing.T, succeed bool) Snapshot {
	t.Helper()
	handle := os.Getenv("BBS_REGRESSION_HANDLE")
	password := os.Getenv("BBS_REGRESSION_PASSWORD")
	first := b.snapshot(t)
	loginPrompt := regexp.MustCompile(`(?i)(handle|name|login|press enter)`)
	if strings.Contains(strings.ToLower(first.Text), "journey onward") {
		first = b.send(t, "1{enter}")
	}
	if !loginPrompt.MatchString(first.Text) {
		first = b.wait(t, `(?i)(handle|name|login|press enter)`, 20)
	}
	if !loginPrompt.MatchString(first.Text) {
		t.Fatalf("no login prompt on screen:\n%s", first.Text)
	}
	if strings.Contains(strings.ToLower(first.Text), "press enter") {
		first = b.send(t, "{enter}")
	}
	first = b.send(t, escapeBraces(handle)+"{enter}")
	first = b.wait(t, `(?i)password`, 20)
	if first.Matched == nil || !*first.Matched {
		t.Fatalf("password prompt not reached:\n%s", first.Text)
	}
	if succeed {
		first = b.send(t, escapeBraces(password)+"{enter}")
	} else {
		// Leave the caller at the password prompt for the negative case.
		return first
	}
	if strings.Contains(strings.ToLower(first.Text), "last caller list") {
		first = b.send(t, "n{enter}")
	}
	return first
}

func (b *regressionBoard) send(t *testing.T, input string) Snapshot {
	t.Helper()
	return b.call(t, "send", map[string]any{"session": b.session, "input": input})
}

func (b *regressionBoard) sendExpect(t *testing.T, input, pattern string, timeout int) Snapshot {
	t.Helper()
	got := b.send(t, input)
	if regexp.MustCompile(pattern).MatchString(got.Text) {
		return got
	}
	return b.wait(t, pattern, timeout)
}

func (b *regressionBoard) wait(t *testing.T, pattern string, timeout int) Snapshot {
	t.Helper()
	got := b.call(t, "wait", map[string]any{"session": b.session, "until": pattern, "timeout_s": timeout})
	if got.Matched == nil || !*got.Matched {
		t.Fatalf("screen did not match %q:\n%s", pattern, got.Text)
	}
	return got
}

func (b *regressionBoard) snapshot(t *testing.T) Snapshot {
	t.Helper()
	return b.call(t, "screen", map[string]any{"session": b.session})
}

func (b *regressionBoard) call(t *testing.T, name string, args map[string]any) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	result, err := b.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("MCP %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("MCP %s: %s", name, result.Content[0].(*mcp.TextContent).Text)
	}
	var got Snapshot
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatalf("MCP %s response: %v", name, err)
	}
	return got
}

func escapeBraces(text string) string { return strings.ReplaceAll(text, "{", "{{") }
