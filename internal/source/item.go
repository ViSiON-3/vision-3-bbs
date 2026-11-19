package source

import "time"

// Item is one post, comment or story as a source definition reads it off a
// page, whether by selectors or by a script running in the page. It is the
// only shape the importer sees.
type Item struct {
	ID       string    `json:"id"`        // stable on the site; becomes "<id>@<msgid_suffix>"
	ParentID string    `json:"parent_id"` // empty for a thread's first item
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	PostedAt time.Time `json:"posted_at"`
	Body     string    `json:"body"`   // HTML
	URL      string    `json:"url"`    // canonical link shown in the message
	Thread   string    `json:"thread"` // page holding the replies
	Link     string    `json:"link"`   // a link post's outbound link
	Marker   string    `json:"marker"` // changes when the replies change, such as a comment count
}
