package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func TestCountPurgeLimitedAreas(t *testing.T) {
	areas := []*message.MessageArea{
		{Tag: "A", MaxMessages: 500},
		{Tag: "B", MaxAge: 365},
		{Tag: "C"},
	}
	if got := countPurgeLimitedAreas(areas); got != 2 {
		t.Errorf("countPurgeLimitedAreas = %d, want 2", got)
	}
}
