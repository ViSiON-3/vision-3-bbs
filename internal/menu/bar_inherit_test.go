package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// barEnv is an executor over a menu set of its own, <tmp>/menus/t with the
// overlay <tmp>/menus.d/t, whose theme and board colours both set the
// shared highlight. write puts a BAR in the shipped set or the overlay.
func barEnv(t *testing.T) (e *MenuExecutor, write func(overlay bool, name, content string), remove func(overlay bool, name string)) {
	t.Helper()
	root := t.TempDir()
	set := menuset.FromPath(filepath.Join(root, "menus", "t"))
	e = &MenuExecutor{MenuSetPath: set.Base}
	e.SetTheme(config.ThemeConfig{YesNoHighlightColor: 31, LightbarSelected: config.SetColor(47)})
	e.SetServerConfig(config.ServerConfig{Theme: config.ThemeSelection{Colors: config.ThemeColors{LightbarSelected: config.SetColor(78)}}})
	path := func(overlay bool, name string) string {
		layer := menuset.LayerBase
		if overlay {
			layer = menuset.LayerOverlay
		}
		return set.Path(layer, "bar", name+".BAR")
	}
	write = func(overlay bool, name, content string) {
		t.Helper()
		p := path(overlay, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	remove = func(overlay bool, name string) {
		t.Helper()
		if err := os.Remove(path(overlay, name)); err != nil {
			t.Fatal(err)
		}
	}
	return e, write, remove
}

func hiBar(colour string) string {
	return "; highlight\n0,0," + colour + ",7,*,*,Highlight\n"
}

// The colour precedence at a screen: its own BAR wins over the board's
// override and the theme; with no BAR the board's override shows; and an
// inherit marker in the overlay returns the screen to the shared colour
// even though the theme ships a BAR for it.
func TestScreenBarOverridesAndInherits(t *testing.T) {
	e, write, remove := barEnv(t)
	shared := colorCodeToAnsi(78) // the board's override of lightbarSelected
	hi := func() string { return resolveAreaHiColor(e, "MSGAREAHI", 1) }

	if got := hi(); got != shared {
		t.Fatalf("no BAR: %q, want the board's colour %q", got, shared)
	}

	// A shipped BAR: an explicit screen override, over board and theme.
	write(false, "MSGAREAHI", hiBar("79"))
	if got := hi(); got != colorCodeToAnsi(79) {
		t.Errorf("shipped BAR: %q, want colour 79", got)
	}

	// The sysop's own BAR in the overlay wins over the shipped one.
	write(true, "MSGAREAHI", hiBar("30"))
	if got := hi(); got != colorCodeToAnsi(30) {
		t.Errorf("overlay BAR: %q, want the sysop's colour 30", got)
	}

	// Inherit shared colours: the marker shadows the shipped BAR.
	write(true, "MSGAREAHI", menuset.BarInheritFile)
	if got := hi(); got != shared {
		t.Errorf("inherit marker over a shipped BAR: %q, want the shared colour %q", got, shared)
	}
	// And follows the shared colour as it changes.
	e.SetServerConfig(config.ServerConfig{})
	if got := hi(); got != colorCodeToAnsi(47) {
		t.Errorf("inherit marker, board override removed: %q, want the theme's 47", got)
	}

	// Deleting the overlay file is not inheriting: the shipped BAR is back.
	remove(true, "MSGAREAHI")
	if got := hi(); got != colorCodeToAnsi(79) {
		t.Errorf("overlay removed: %q, want the shipped BAR's 79 again", got)
	}
}

// Every screen with a highlight BAR treats the marker the same way: the
// loader returns no override for it, with or without comments around it
// and whatever its case.
func TestInheritMarkerLoadsAsNoOverride(t *testing.T) {
	e, write, _ := barEnv(t)
	for _, sb := range menuset.ScreenBars {
		write(false, sb.Name, hiBar("79"))
		if opts, err := loadBarFile(sb.Name, e); err != nil || len(opts) != 1 || opts[0].HighlightColor != 79 {
			t.Fatalf("%s: shipped BAR loads as %+v, %v", sb.Name, opts, err)
		}
		for _, marker := range []string{menuset.BarInheritFile, "inherit\n", "; note\n\n  Inherit  \n; more\n"} {
			write(true, sb.Name, marker)
			if opts, err := loadBarFile(sb.Name, e); err != nil || opts != nil {
				t.Errorf("%s with marker %q: %+v, %v; want no override", sb.Name, marker, opts, err)
			}
		}
	}
	if opts, err := loadBarFile("NOSUCH", e); err != nil || opts != nil {
		t.Errorf("missing BAR: %+v, %v", opts, err)
	}
}

// The stock theme no longer ships FILELISTHI, MSGAREAHI or MSGCONFHI, which
// only repeated the shared highlight. Their screens draw the highlight in
// the colour those files held, 31, so nothing looks different.
func TestRetiredStockBarsLeaveTheLookUnchanged(t *testing.T) {
	stock := stockMenuSet(t)
	for _, name := range []string{"FILELISTHI", "MSGAREAHI", "MSGCONFHI"} {
		if _, err := os.Stat(filepath.Join(stock, "bar", name+".BAR")); !os.IsNotExist(err) {
			t.Errorf("%s.BAR is still shipped (stat err %v)", name, err)
		}
	}
	theme, err := config.LoadThemeConfig(stock)
	if err != nil {
		t.Fatal(err)
	}
	retired := colorCodeToAnsi(31) // what the three BARs drew
	if got := colorCodeToAnsi(theme.Color(config.RoleLightbarSelected)); got != retired {
		t.Fatalf("stock lightbarSelected draws %q, the retired BARs drew %q", got, retired)
	}

	for _, sc := range []struct {
		name, cmd, input string
		env              func(t *testing.T) *menuEnv
	}{
		{"message area selector", "SELECTMSGAREA", "q", newMsgEnv},
		{"message conference selector", "CHANGEMSGCONF", "q", newMsgEnv},
		{"file list", "LISTFILES", "q", func(t *testing.T) *menuEnv {
			env := lightbarListEnv(t)
			addReviewUpload(t, env, 1, "A.ZIP", 4)
			return env
		}},
	} {
		t.Run(sc.name, func(t *testing.T) {
			env := sc.env(t)
			env.e.SetTheme(theme)
			r := env.runCmd(sc.cmd, env.sysop, "", sc.input)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if !strings.Contains(r.raw, retired) {
				t.Errorf("no highlight in colour 31:\n%q", r.raw)
			}

			// And the screen now follows the shared colour.
			changed := theme
			changed.LightbarSelected = config.SetColor(78)
			env = sc.env(t)
			env.e.SetTheme(changed)
			if r = env.runCmd(sc.cmd, env.sysop, "", sc.input); !strings.Contains(r.raw, colorCodeToAnsi(78)) {
				t.Errorf("highlight didn't follow lightbarSelected:\n%q", r.raw)
			}
		})
	}
}

// The stock theme no longer ships FILELISTCMD.BAR (#1092): the file list
// builds the same commands, and colours them with the lightbar roles, so a
// change to lightbarSelected or lightbarNormal reaches the command bar. The
// unselected commands of a stock board draw lightbarNormal, 13, where the
// retired file had 7: a deliberate change, matching the management bar and
// the message scan's bar.
func TestFileListCommandBarFollowsTheRoles(t *testing.T) {
	stock := stockMenuSet(t)
	if _, err := os.Stat(filepath.Join(stock, "bar", "FILELISTCMD.BAR")); !os.IsNotExist(err) {
		t.Errorf("FILELISTCMD.BAR is still shipped (stat err %v)", err)
	}
	theme, err := config.LoadThemeConfig(stock)
	if err != nil {
		t.Fatal(err)
	}
	run := func(theme config.ThemeConfig) string {
		t.Helper()
		env := lightbarListEnv(t)
		addReviewUpload(t, env, 1, "A.ZIP", 4)
		env.e.SetTheme(theme)
		r := env.runCmd("LISTFILES", env.caller, "", "q")
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.raw
	}
	cmd := func(colour int, label string) string { return colorCodeToAnsi(colour) + " " + label + " " }

	out := run(theme)
	if !strings.Contains(out, cmd(31, "Mark")) || !strings.Contains(out, cmd(13, "Quit")) {
		t.Errorf("stock command bar: want Mark selected in 31 and Quit in 13:\n%q", out)
	}

	changed := theme
	changed.LightbarSelected, changed.LightbarNormal = config.SetColor(78), config.SetColor(45)
	out = run(changed)
	for _, want := range []string{cmd(78, "Mark"), cmd(45, "Info"), cmd(45, "Download"), cmd(45, "Quit")} {
		if !strings.Contains(out, want) {
			t.Errorf("command bar has no %q under changed roles:\n%q", want, out)
		}
	}
}

// A sysop's own FILELISTCMD.BAR still defines the command bar, commands
// and colours both, whatever the roles are.
func TestFileListCommandBarFromASysopsBar(t *testing.T) {
	e, write, _ := barEnv(t)
	e.SetTheme(config.ThemeConfig{LightbarSelected: config.SetColor(78), LightbarNormal: config.SetColor(45)})
	write(true, "FILELISTCMD", "; mine\n0,0,79,3,i,info,Info\n0,0,79,3,q,quit,Leave\n")
	opts, err := loadBarFile("FILELISTCMD", e)
	if err != nil {
		t.Fatal(err)
	}
	cmds, _, _, _, _ := buildFileListCmdBar(e, false, opts, nil)
	if len(cmds) != 2 || cmds[1].label != "Leave" || cmds[0].highlightColor != colorCodeToAnsi(79) || cmds[0].regularColor != colorCodeToAnsi(3) {
		t.Errorf("command bar = %+v; want the BAR's two commands in 79 and 3", cmds)
	}
	cmds, _, _, _, _ = buildFileListCmdBar(e, false, nil, nil)
	if len(cmds) != 6 || cmds[0].highlightColor != colorCodeToAnsi(78) || cmds[0].regularColor != colorCodeToAnsi(45) {
		t.Errorf("without a BAR = %+v; want the six built-in commands in the roles", cmds)
	}
}
