package ftn

import (
	"strings"
	"testing"
)

// tqwNet's tqw_file.na, in the FILEBONE.NA shape.
const tqwFileNA = "% tqwNet file echoes\r\n" +
	"Area TQW_NODE\t\t0\t!\tWeekly Nodelists\r\n" +
	"Area TQW_INFO\t\t0\t!\tWeekly Infopacks\r\n" +
	"Area TQW_MYSUTILS\t0\t!\tMystic BBS Utils/Doors/Games/etc\r\n" +
	"Area TQW_LINUXFILES 0 ! Linux files\r\n"

func TestParseFileEchoListFilebone(t *testing.T) {
	areas, err := ParseFileEchoList(strings.NewReader(tqwFileNA))
	if err != nil {
		t.Fatal(err)
	}
	want := []EchoArea{
		{"TQW_NODE", "Weekly Nodelists"},
		{"TQW_INFO", "Weekly Infopacks"},
		{"TQW_MYSUTILS", "Mystic BBS Utils/Doors/Games/etc"},
		{"TQW_LINUXFILES", "Linux files"},
	}
	if len(areas) != len(want) {
		t.Fatalf("got %+v", areas)
	}
	for i := range want {
		if areas[i] != want[i] {
			t.Errorf("area %d = %+v, want %+v", i, areas[i], want[i])
		}
	}
}

func TestParseFileEchoListPlain(t *testing.T) {
	areas, err := ParseFileEchoList(strings.NewReader("; comment\nTQW_DEMOS  - Demoscene\nTQW_TEST Testing only\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(areas) != 2 || areas[0] != (EchoArea{"TQW_DEMOS", "Demoscene"}) || areas[1] != (EchoArea{"TQW_TEST", "Testing only"}) {
		t.Errorf("got %+v", areas)
	}
}

// A level column but no flags column, and a tag with no description.
func TestParseFileEchoListOptionalColumns(t *testing.T) {
	areas, err := ParseFileEchoList(strings.NewReader("Area FSX_DAT 10 FSX data files\nArea FSX_NODE\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(areas) != 2 || areas[0] != (EchoArea{"FSX_DAT", "FSX data files"}) || areas[1] != (EchoArea{"FSX_NODE", ""}) {
		t.Errorf("got %+v", areas)
	}
}

func TestParseFileEchoListRejectsRepeats(t *testing.T) {
	if _, err := ParseFileEchoList(strings.NewReader("TQW_NODE a\ntqw_node b\n")); err == nil {
		t.Error("a repeated tag was accepted")
	}
	if _, err := ParseFileEchoList(strings.NewReader("; nothing\n")); err == nil {
		t.Error("an empty list was accepted")
	}
}
