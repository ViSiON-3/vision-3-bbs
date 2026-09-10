package mailer

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// TestReloadRaceWithExport reloads ftn.json concurrently with export cycles and
// snapshot reads. Run under -race it proves the snapshot swap in reloadFTN is
// safe against the reads in exportOnce/currentFTN (issue #267).
func TestReloadRaceWithExport(t *testing.T) {
	dir := t.TempDir()
	writeFTN := func(port int) {
		// No tosser-enabled network, so ValidateFTNConfig passes without global
		// paths, and exportOnce has nothing to toss — the loop still reads the
		// snapshot each iteration, which is what we are racing.
		body := `{"binkd":{"port":` + itoa(port) + `},"networks":{}}`
		if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(body), 0o644); err != nil {
			t.Fatalf("write ftn.json: %v", err)
		}
	}
	writeFTN(24554)

	dupe, err := tosser.NewDupeDB(os.DevNull, 0)
	if err != nil {
		t.Fatalf("dupe db: %v", err)
	}
	s := &Service{
		cfg:          Config{BBSRoot: dir},
		configDir:    dir,
		exportDupeDB: dupe,
	}
	s.ftn.Store(&config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{}})

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: rewrite ftn.json and reload it, swapping the snapshot.
	wg.Add(1)
	go func() {
		defer wg.Done()
		port := 24554
		for {
			select {
			case <-stop:
				return
			default:
				port++
				writeFTN(port)
				s.reloadFTN()
			}
		}
	}()

	// Readers: an export cycle and repeated snapshot reads.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.exportOnce()
					_ = s.currentFTN().Binkd.Port
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// itoa avoids importing strconv just for the test fixture.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
