package configeditor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

func TestFTNWizard_ListRetryRejectsCancelledAttempt(t *testing.T) {
	for _, files := range []bool{false, true} {
		for _, staleFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("files=%v/failure=%v", files, staleFailure), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls == 1 && staleFailure {
						http.Error(w, "old failure", http.StatusInternalServerError)
						return
					}
					fmt.Fprintln(w, "TEST_FILES 0 Test files")
				}))
				defer server.Close()
				m := wizardReadyToSave(t)
				m.ftnWizard.echolistURL = server.URL
				m.ftnWizard.fileEchoListURL = server.URL
				m.ftnWizard.areasFetched = false
				enter := Model.enterFTNAreaBrowser
				if files {
					enter = Model.enterFTNFileEchoBrowser
				}
				m, oldCmd := enter(m)
				m = press(t, m, "esc")
				m, currentCmd := enter(m)
				m = asModel(t, first(m.Update(oldCmd())))
				if m.mode != modeFTNAreaDownloading || !m.ftnAreaBrowserLoading || m.ftnAreaBrowserError != "" {
					t.Fatalf("cancelled result replaced pending download: mode=%v error=%q", m.mode, m.ftnAreaBrowserError)
				}
				m = asModel(t, first(m.Update(currentCmd())))
				if m.mode != modeFTNAreaBrowser || len(m.ftnAreaBrowserAreas) != 1 || m.ftnAreaBrowserError != "" {
					t.Fatalf("current result not accepted: mode=%v areas=%v error=%q", m.mode, m.ftnAreaBrowserAreas, m.ftnAreaBrowserError)
				}
			})
		}
	}
}

func TestFTNWizard_FileEchoFailureClearsPreviousBrowser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()
	m := wizardReadyToSave(t)
	m.ftnWizard.availableAreas = []ftn.EchoArea{{Tag: "OLD_ECHO"}}
	m.ftnWizard.selectedAreas = []bool{true}
	m = m.openFTNAreaBrowser(false)
	m = press(t, m, "esc")
	m.ftnWizard.fileEchoListURL = server.URL
	m, cmd := m.enterFTNFileEchoBrowser()
	m = asModel(t, first(m.Update(cmd())))
	if len(m.ftnAreaBrowserAreas) != 0 || len(m.ftnAreaBrowserSelected) != 0 {
		t.Fatalf("failed file download retained previous browser: %v", m.ftnAreaBrowserAreas)
	}
	wantScreen(t, m, "Download failed")
	result, retry := m.updateFTNAreaBrowser(keyMsg("r"))
	m = asModel(t, result)
	if retry == nil || m.mode != modeFTNAreaDownloading {
		t.Fatal("retry unavailable after failure")
	}
	if len(m.ftnWizard.availableAreas) != 1 || !m.ftnWizard.selectedAreas[0] {
		t.Fatal("echomail cache changed")
	}
}
