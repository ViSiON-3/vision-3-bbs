package nntp

import (
	"errors"
	"strings"
	"testing"
)

func articleRaw(id, refs, subject, body string) []byte {
	return []byte("Message-ID: <" + id + "@example.test>\r\nFrom: =?utf-8?Q?Retro_Pilot?= <pilot@example.invalid>\r\nSubject: " + subject + "\r\nDate: Thu, 29 Oct 2026 14:00:01 +0000\r\nReferences: " + refs + "\r\n" + body)
}
func TestParseArticleMIMEAndReferences(t *testing.T) {
	a, err := ParseArticle(articleRaw("a", "<root@example.test>", "=?iso-8859-1?Q?Caf=E9?=", "Content-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n> quoted\r\nCaf=E9 =1B[2J\r\n"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if a.From != "Retro Pilot" || a.Subject != "Café" || !strings.Contains(a.Body, "Café") || strings.Contains(a.Body, "\x1b") || a.Date.Year() != 2026 || len(a.References) != 1 {
		t.Fatalf("article=%+v", a)
	}
}
func TestArticleMultipartPrefersInlinePlain(t *testing.T) {
	body := "Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=secret.txt\r\n\r\nattachment\r\n--x\r\nContent-Type: multipart/alternative; boundary=y\r\n\r\n--y\r\nContent-Type: text/html\r\n\r\n<p>HTML fallback</p>\r\n--y\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\ncGxhaW4gdGV4dA==\r\n--y--\r\n--x--\r\n"
	a, err := ParseArticle(articleRaw("a", "", "title", body), 4096)
	if err != nil || a.Body != "plain text" {
		t.Fatalf("body=%q err=%v", a.Body, err)
	}
}
func TestArticleRejectsBinaryAndBadIDs(t *testing.T) {
	_, err := ParseArticle(articleRaw("a", "", "file", "Content-Type: application/octet-stream\r\n\r\nbinary"), 4096)
	if !errors.Is(err, ErrNoText) {
		t.Fatalf("binary err=%v", err)
	}
	_, err = ParseArticle([]byte("Message-ID: not-an-id\r\n\r\nbody"), 4096)
	if err == nil {
		t.Fatal("accepted malformed ID")
	}
	_, err = ParseArticle(articleRaw("a", "", "large", "\r\n"+strings.Repeat("x", 100)), 10)
	if err == nil {
		t.Fatal("accepted oversized decoded text")
	}
}

func TestLegacyBBSCharsets(t *testing.T) {
	for _, label := range []string{"cp437", "IBM437"} {
		a, err := ParseArticle(articleRaw("a", "", "BBS art", "Content-Type: text/plain; charset="+label+"\r\n\r\n\xdb\xdc"), 4096)
		if err != nil || a.Body != "█▄" {
			t.Fatalf("%s: body=%q err=%v", label, a.Body, err)
		}
	}
	a, err := ParseArticle(articleRaw("a", "", "BBS gate", "Content-Type: text/plain; charset=us - ascii\r\n\r\nold school"), 4096)
	if err != nil || a.Body != "old school" {
		t.Fatalf("body=%q err=%v", a.Body, err)
	}
}
