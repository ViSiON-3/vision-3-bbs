package reddit

import "testing"

func TestHTMLToText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"paragraphs", "<p>One</p><p>Two</p>", "One\n\nTwo"},
		{"line break", "<p>a<br>b</p>", "a\nb"},
		{"link", `<p>See <a href="https://x.org">the site</a></p>`, "See the site <https://x.org>"},
		{"bare link", `<a href="https://x.org">https://x.org</a>`, "https://x.org"},
		{"list", "<ul><li>one</li><li>two</li></ul>", "- one\n- two"},
		{"entities", "<p>a &amp; b &lt;c&gt; &#9731;</p>", "a & b <c> ☃"},
		{"whitespace", "<p>  lots\n   of   space </p>", "lots of space"},
		{"code kept", "<pre><code>x :=  1\n  y</code></pre>", "x :=  1\n  y"},
		{"quote", "<blockquote><p>said</p></blockquote>", "> said"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTMLToText(tt.in); got != tt.want {
				t.Errorf("HTMLToText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
