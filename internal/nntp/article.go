package nntp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/source"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/charmap"
)

// ErrNoText reports an article with no inline text part in a supported type.
var ErrNoText = errors.New("article has no supported inline text")
var messageID = regexp.MustCompile(`^<[^<>\s@]+@[^<>\s@]+>$`)
var referenceIDs = regexp.MustCompile(`<[^<>\s@]+@[^<>\s@]+>`)
var brokenASCIICharset = regexp.MustCompile(`(?i)\bus\s+-\s+ascii\b`)

func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "cp437", "ibm437", "ibm-437", "437":
		return charmap.CodePage437.NewDecoder().Reader(input), nil
	default:
		return charset.NewReaderLabel(label, input)
	}
}

// Article is one parsed article: its Message-ID, the headers shown to
// callers, the IDs it replies to and its body as plain text.
type Article struct {
	ID, From, Subject, Body string
	References              []string
	Date                    time.Time
}

func validID(s string) bool { return len(s) <= 250 && messageID.MatchString(s) }
func plain(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, ansi.StripAnsi(s))
}
func header(s string) string {
	decoder := mime.WordDecoder{CharsetReader: charsetReader}
	decoded, err := decoder.DecodeHeader(s)
	if err == nil {
		s = decoded
	}
	return strings.Join(strings.Fields(plain(s)), " ")
}

// ParseArticle parses a raw article of at most limit bytes of body text. It
// rejects an article whose Message-ID is missing or malformed, and returns
// ErrNoText when no part is inline text.
func ParseArticle(raw []byte, limit int) (Article, error) {
	var a Article
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return a, err
	}
	a.ID = strings.TrimSpace(m.Header.Get("Message-ID"))
	if !validID(a.ID) {
		return a, fmt.Errorf("missing/invalid Message-ID")
	}
	if m.Header.Get("Control") != "" {
		return a, ErrNoText
	}
	a.From = header(m.Header.Get("From"))
	if parsed, e := mail.ParseAddress(a.From); e == nil {
		if parsed.Name != "" {
			a.From = parsed.Name
		} else {
			a.From = parsed.Address
		}
	}
	a.Subject = header(m.Header.Get("Subject"))
	if a.Subject == "" {
		a.Subject = "(no subject)"
	}
	if a.From == "" {
		a.From = "Usenet"
	}
	a.Date, _ = mail.ParseDate(m.Header.Get("Date"))
	// Some older newsreaders omit the numeric time zone. Keep their stated
	// wall time in UTC rather than silently replacing a decades-old date.
	if a.Date.IsZero() {
		for _, layout := range []string{"Mon, 2 Jan 2006 15:04:05", "2 Jan 2006 15:04:05"} {
			if t, e := time.Parse(layout, m.Header.Get("Date")); e == nil {
				a.Date = t
				break
			}
		}
	}
	refs := referenceIDs.FindAllString(m.Header.Get("References"), -1)
	for _, id := range refs {
		if validID(id) {
			a.References = append(a.References, id)
		}
	}
	if len(a.References) > 64 {
		a.References = a.References[len(a.References)-64:]
	}
	if len(a.References) == 0 {
		a.References = referenceIDs.FindAllString(m.Header.Get("In-Reply-To"), 1)
	}
	a.Body, _, err = decodePart(textproto.MIMEHeader(m.Header), m.Body, limit, 0)
	a.Body = strings.TrimSpace(plain(a.Body))
	if err == nil && a.Body == "" {
		err = ErrNoText
	}
	return a, err
}
func decodePart(h textproto.MIMEHeader, r io.Reader, limit, depth int) (string, bool, error) {
	if depth > 4 {
		return "", false, ErrNoText
	}
	// Some long-running BBS-to-Usenet gates emit the invalid token
	// "charset=us - ascii". Normalize this known spelling before MIME parsing.
	contentType := brokenASCIICharset.ReplaceAllString(h.Get("Content-Type"), "us-ascii")
	kind, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		if h.Get("Content-Type") != "" {
			return "", false, err
		}
		kind = "text/plain"
	}
	disposition, _, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	if disposition == "attachment" {
		return "", false, ErrNoText
	}
	switch strings.ToLower(h.Get("Content-Transfer-Encoding")) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "", "7bit", "8bit", "binary":
	default:
		return "", false, ErrNoText
	}
	if strings.HasPrefix(kind, "multipart/") {
		if params["boundary"] == "" {
			return "", false, fmt.Errorf("missing MIME boundary")
		}
		mr := multipart.NewReader(r, params["boundary"])
		fallback := ""
		for parts := 0; parts < 32; parts++ {
			p, e := mr.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", false, e
			}
			text, isPlain, e := decodePart(p.Header, p, limit, depth+1)
			_ = p.Close()
			if e == nil {
				if isPlain {
					return text, true, nil
				}
				if fallback == "" {
					fallback = text
				}
			}
		}
		if fallback != "" {
			return fallback, false, nil
		}
		return "", false, ErrNoText
	}
	if kind != "text/plain" && kind != "text/html" {
		return "", false, ErrNoText
	}
	label := params["charset"]
	if label != "" && !strings.EqualFold(label, "utf-8") && !strings.EqualFold(label, "us-ascii") {
		r, err = charsetReader(label, r)
		if err != nil {
			return "", false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > limit {
		return "", false, fmt.Errorf("decoded article exceeds size limit")
	}
	text := strings.ToValidUTF8(string(data), "�")
	if kind == "text/html" {
		text = source.HTMLToText(text)
	}
	return text, kind == "text/plain", nil
}
