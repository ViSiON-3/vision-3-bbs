package qwkapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRequireClient(t *testing.T) {
	h := requireClient(okHandler)

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"missing header", map[string]string{}, http.StatusNotFound},
		{"present", map[string]string{"X-V3-Client": "vision3-mobile"}, http.StatusOK},
		{"browser sec-fetch", map[string]string{"X-V3-Client": "x", "Sec-Fetch-Mode": "navigate"}, http.StatusNotFound},
		{"browser accept html", map[string]string{"X-V3-Client": "x", "Accept": "text/html,application/xhtml+xml"}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/qwk/packet", nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			rr := httptest.NewRecorder()
			h(rr, req)
			if rr.Code != c.want {
				t.Errorf("status = %d, want %d", rr.Code, c.want)
			}
		})
	}
}
