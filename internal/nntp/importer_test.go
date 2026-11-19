package nntp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/source"
)

type fakeReader struct {
	low, high           int64
	data                map[int64][]byte
	fail                int64
	rangeLow, rangeHigh int64
	reads               int
}

func (f *fakeReader) Group(string) (int64, int64, error) { return f.low, f.high, nil }
func (f *fakeReader) Overview(low, high int64) ([]Overview, error) {
	f.rangeLow, f.rangeHigh = low, high
	var rows []Overview
	for n := low; n <= high; n++ {
		if _, ok := f.data[n]; ok {
			rows = append(rows, Overview{Number: n})
		}
	}
	return rows, nil
}
func (f *fakeReader) Article(n int64) ([]byte, error) {
	f.reads++
	if n == f.fail {
		return nil, errors.New("network interrupted")
	}
	if b, ok := f.data[n]; ok {
		return b, nil
	}
	return nil, ErrMissing
}
func (f *fakeReader) Close() error { return nil }
func newImportTest(t *testing.T) (*Importer, *fakeReader, Group) {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "configs")
	data := filepath.Join(root, "data")
	os.MkdirAll(cfg, 0755)
	os.MkdirAll(data, 0755)
	os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(`[{"id":1,"position":1,"tag":"NNTP_TEST","name":"Test","area_type":"local","acs_write":"S256","base_path":"msgbases/test"}]`), 0600)
	mm, err := message.NewMessageManager(data, cfg, "Test", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := source.OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	r := &fakeReader{low: 1, high: 3, data: map[int64][]byte{1: articleRaw("root", "", "Thread", "\r\nroot body"), 3: articleRaw("reply", "<root@example.test>", "Re: Thread", "\r\nreply body")}}
	return &Importer{Config: testConfig(), Reader: r, Messages: mm, State: state, Out: io.Discard}, r, Group{Name: "comp.sys.test", AreaTag: "NNTP_TEST", Enabled: true}
}
func TestImporterDedupDatesThreadsAndCheckpoint(t *testing.T) {
	im, r, g := newImportTest(t)
	res := im.Sync(g)
	if res.Err != nil || res.Imported != 2 || res.Through != 3 {
		t.Fatalf("result=%+v", res)
	}
	parent, err := im.Messages.GetMessage(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	child, err := im.Messages.GetMessage(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if parent.MsgID != "<root@example.test>" || parent.DateTime.Year() != 2026 || child.ReplyID != parent.MsgID || child.ReplyToNum != 1 {
		t.Fatalf("parent=%+v child=%+v", parent, child)
	}
	if res = im.Sync(g); res.Err != nil || res.Imported != 0 || r.reads != 2 {
		t.Fatalf("second=%+v reads=%d", res, r.reads)
	}
	// A lost checkpoint still does not duplicate articles already in JAM.
	im.State.SetMarker("nntp", im.Config.Server+"/"+g.Name, "high_water", "0")
	if res = im.Sync(g); res.Err != nil || res.Imported != 0 {
		t.Fatalf("replay=%+v", res)
	}
	tree, err := im.Messages.ReplyTree(1)
	if err != nil || tree[2].Parent != 1 {
		t.Fatalf("tree=%+v err=%v", tree, err)
	}
}
func TestImporterFailureDoesNotAdvanceAndDryRunDoesNotWrite(t *testing.T) {
	im, r, g := newImportTest(t)
	im.DryRun = true
	if res := im.Sync(g); res.Err != nil || res.Imported != 2 {
		t.Fatal(res)
	}
	if _, seen, _ := im.State.Marker("nntp", im.Config.Server+"/"+g.Name, "high_water"); seen {
		t.Fatal("dry run advanced checkpoint")
	}
	if n := im.Messages.FindMessageByMSGID(1, "<root@example.test>"); n != 0 {
		t.Fatal("dry run wrote a message")
	}
	im.DryRun = false
	r.fail = 3
	if res := im.Sync(g); res.Err == nil {
		t.Fatal("network failure ignored")
	}
	if _, seen, _ := im.State.Marker("nntp", im.Config.Server+"/"+g.Name, "high_water"); seen {
		t.Fatal("failed run advanced checkpoint")
	}
	r.fail = 0
	if res := im.Sync(g); res.Err != nil || res.Imported != 2 {
		t.Fatal(res)
	}
}
func TestImporterInitialWindowAndCatchUp(t *testing.T) {
	im, r, g := newImportTest(t)
	r.high = 100
	r.data = map[int64][]byte{}
	for n := int64(1); n <= 110; n++ {
		r.data[n] = articleRaw(strconv.FormatInt(n, 10), "", fmt.Sprint(n), "\r\nbody")
	}
	im.Config.InitialArticles = 3
	im.Config.MaxArticles = 3
	if res := im.Sync(g); res.Err != nil || res.Imported != 3 || r.rangeLow != 98 || r.rangeHigh != 100 {
		t.Fatal(res, r.rangeLow, r.rangeHigh)
	}
	r.high = 110
	if res := im.Sync(g); res.Err != nil || res.Imported != 3 || r.rangeLow != 101 || r.rangeHigh != 103 {
		t.Fatal(res, r.rangeLow, r.rangeHigh)
	}
}

func TestImporterLateParentAndServerRenumber(t *testing.T) {
	im, r, g := newImportTest(t)
	r.high = 2
	r.data = map[int64][]byte{1: articleRaw("reply", "<root@example.test>", "Re: Thread", "\r\nchild first"), 2: articleRaw("root", "", "Thread", "\r\nparent later")}
	if res := im.Sync(g); res.Err != nil || res.Imported != 2 {
		t.Fatal(res)
	}
	tree, err := im.Messages.ReplyTree(1)
	if err != nil || tree[1].Parent != 2 {
		t.Fatalf("late parent tree=%+v err=%v", tree, err)
	}
	im.State.SetMarker("nntp", im.Config.Server+"/"+g.Name, "high_water", "1000")
	if res := im.Sync(g); res.Err != nil || res.Imported != 0 || res.Through != 2 {
		t.Fatal(res)
	}
}

func TestImporterRequiresReadOnlyLocalArea(t *testing.T) {
	for _, kind := range []string{"writable", "networked"} {
		t.Run(kind, func(t *testing.T) {
			im, _, g := newImportTest(t)
			area, _ := im.Messages.GetAreaByTag(g.AreaTag)
			if kind == "writable" {
				area.ACSWrite = "s10"
			} else {
				area.AreaType = "v3net"
			}
			if result := im.Sync(g); result.Err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}
