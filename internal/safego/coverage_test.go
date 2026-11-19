package safego

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sessionPackages are the directories, from the module root, whose code runs
// for one caller's session. A goroutine started there must be started with Go.
var sessionPackages = []string{
	"cmd/vision3",
	"internal/editor",
	"internal/menu",
	"internal/scripting",
	"internal/sshserver",
	"internal/syncjs",
	"internal/telnetserver",
	"internal/transfer",
	"internal/winch",
}

// boardWide are the go statements on those paths that serve no one caller,
// by file and enclosing function, with why they are left alone.
var boardWide = map[string]string{
	"cmd/vision3/main.go:main":                                 "listeners, scheduler, V3Net, mailer and admin services for the whole board",
	"cmd/vision3/main.go:(*ConnectionTracker).startWatching":   "watches the block and allow lists for the whole board",
	"cmd/vision3/config_watcher.go:NewConfigWatcher":           "reloads configuration for the whole board",
	"cmd/vision3/ssh_server.go:startSSHServer":                 "the SSH accept loop; each session it starts is recovered in sshserver",
	"internal/telnetserver/server.go:(*Server).ListenAndServe": "the telnet accept loop; handleConnection recovers each session it starts",
}

// TestNoBareGoOnSessionPaths fails for a go statement on a session path that
// does not hand its work to Go (go Run(...) in this package is the one
// allowed start).
func TestNoBareGoOnSessionPaths(t *testing.T) {
	root := filepath.Join("..", "..")
	var bare []string
	seen := map[string]bool{}
	for _, dir := range sessionPackages {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files in %s: %v", dir, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			rel := filepath.ToSlash(filepath.Join(dir, filepath.Base(path)))
			for _, g := range goStatements(fset, f, rel) {
				if _, ok := boardWide[g.key]; ok {
					seen[g.key] = true
					continue
				}
				bare = append(bare, g.where)
			}
		}
	}
	sort.Strings(bare)
	if len(bare) > 0 {
		t.Errorf("go statements on session paths not started with safego.Go:\n  %s", strings.Join(bare, "\n  "))
	}
	for key := range boardWide {
		if !seen[key] {
			t.Errorf("boardWide lists %s, which starts no goroutine; take it off", key)
		}
	}
}

// goStmt is a go statement found by goStatements: key is its file and
// enclosing declaration, as boardWide lists them, and where adds the line.
type goStmt struct{ key, where string }

// goStatements lists the go statements in f, which is rel from the module
// root: in functions and in package-level declarations, which can hold
// function literals too (var start = func() { go ... }).
func goStatements(fset *token.FileSet, f *ast.File, rel string) []goStmt {
	var out []goStmt
	for _, d := range f.Decls {
		var body ast.Node = d
		name := "(package level)"
		if fd, ok := d.(*ast.FuncDecl); ok {
			if fd.Body == nil {
				continue
			}
			body, name = fd.Body, funcName(fd)
		}
		key := rel + ":" + name
		ast.Inspect(body, func(n ast.Node) bool {
			if g, ok := n.(*ast.GoStmt); ok {
				out = append(out, goStmt{key: key, where: fmt.Sprintf("%s:%d (%s)", rel, fset.Position(g.Pos()).Line, name)})
			}
			return true
		})
	}
	return out
}

// The guard sees go statements in methods, in nested function literals and
// in package-level ones.
func TestGoStatementsFindsEveryKind(t *testing.T) {
	src := `package p
var start = func() { go work() }
type T struct{}
func (t *T) Run() { f := func() { go work() }; f() }
func work() {}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range goStatements(fset, f, "p.go") {
		got = append(got, g.key)
	}
	want := []string{"p.go:(package level)", "p.go:(*T).Run"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("go statements = %v, want %v", got, want)
	}
}

// funcName is fd's name, with its receiver type for a method.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	var b strings.Builder
	switch t := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			b.WriteString("(*" + id.Name + ")")
		}
	case *ast.Ident:
		b.WriteString(t.Name)
	}
	return b.String() + "." + fd.Name.Name
}

// The test reads the module from two directories up; make sure it is there.
func init() {
	if _, err := os.Stat(filepath.Join("..", "..", "go.mod")); err != nil {
		panic("safego tests must run from internal/safego: " + err.Error())
	}
}
