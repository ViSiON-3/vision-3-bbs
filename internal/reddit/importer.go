package reddit

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// Importer files one subreddit's new posts and comments into a message area.
type Importer struct {
	MM      *message.MessageManager
	Fetcher Fetcher
	Sel     Selectors
	Cfg     Config
	Store   *Store
	Sleep   func(time.Duration) // time.Sleep in production
	Now     func() time.Time    // time.Now when nil
	// Deadline, when set, is when the run must be finished: no page load
	// starts unless the pause before it plus PageTimeout fits before it.
	Deadline time.Time
	DryRun   bool
	Out      io.Writer // where a dry run prints
	fetched  int
}

// SubResult is what one subreddit's sync did.
type SubResult struct {
	Subreddit, AreaTag     string
	Posts, Comments, Pages int
	Err                    error
}

func msgIDFor(redditID string) string { return redditID + "@reddit" }

// PageTimeout bounds one page load.
const PageTimeout = 60 * time.Second

// ErrRunTimeLimit stops a run that could not finish another page load before
// its deadline. What was imported stays; the next run continues from there.
var ErrRunTimeLimit = errors.New("stopped at the run time limit; the next run continues")

func (im *Importer) now() time.Time {
	if im.Now != nil {
		return im.Now()
	}
	return time.Now()
}

// fetch loads a page, pausing between loads.
func (im *Importer) fetch(url string, res *SubResult) (string, error) {
	pause := time.Duration(0)
	if im.fetched > 0 {
		pause = im.Cfg.PageDelay()
	}
	if !im.Deadline.IsZero() && im.now().Add(pause+PageTimeout).After(im.Deadline) {
		return "", ErrRunTimeLimit
	}
	if pause > 0 && im.Sleep != nil {
		im.Sleep(pause)
	}
	im.fetched++
	res.Pages++
	return im.Fetcher.Fetch(url)
}

// SyncSubreddit imports what is new in one subreddit.
func (im *Importer) SyncSubreddit(m Mapping) SubResult {
	res := SubResult{Subreddit: m.Subreddit, AreaTag: m.AreaTag}
	area, ok := im.MM.GetAreaByTag(m.AreaTag)
	if !ok {
		res.Err = fmt.Errorf("message area %s does not exist", m.AreaTag)
		return res
	}
	page, err := im.fetch(im.Sel.ListingURLFor(m.Subreddit), &res)
	if err != nil {
		res.Err = err
		return res
	}
	posts, err := ParseListing(page, im.Sel)
	if err != nil {
		res.Err = err
		return res
	}
	if len(posts) > im.Cfg.MaxPostsPerSync {
		posts = posts[:im.Cfg.MaxPostsPerSync]
	}
	// Oldest first, so post numbers in the area follow posting order.
	for i := len(posts) - 1; i >= 0; i-- {
		if err := im.syncPost(area, m.Subreddit, posts[i], &res); err != nil {
			res.Err = err
			return res
		}
	}
	return res
}

// syncPost imports a post and whatever of its comments are new.
func (im *Importer) syncPost(area *message.MessageArea, sub string, p Post, res *SubResult) error {
	postMsgID := msgIDFor(p.ID)
	known := im.MM.FindMessagesByMSGID(area.ID, []string{postMsgID})
	seen, wasSeen, err := im.Store.Count(sub, p.ID)
	if err != nil {
		return err
	}
	isNew := known[postMsgID] == 0
	if !isNew && wasSeen && p.CommentCount <= seen {
		return nil
	}

	post, comments := p, []Comment(nil)
	if isNew || p.CommentCount > 0 {
		page, err := im.fetch(im.Sel.PageURL(p.Permalink), res)
		if err != nil {
			return err
		}
		pp, cs, err := ParsePostPage(page, im.Sel)
		if err != nil {
			return err
		}
		post.BodyHTML, comments = pp.BodyHTML, cs
	}

	// Look every ID up before opening the base for writing: the MSGID index
	// opens its own handle on the base and must not nest inside ours.
	ids := []string{postMsgID}
	for _, c := range comments {
		ids = append(ids, msgIDFor(c.ID))
	}
	known = im.MM.FindMessagesByMSGID(area.ID, ids)

	if im.DryRun {
		im.dryRun(post, comments, known, res)
		return nil
	}

	base, err := im.MM.GetBase(area.ID)
	if err != nil {
		return err
	}
	defer func() { _ = base.Close() }() // the writes have been checked individually

	if known[postMsgID] == 0 {
		num, err := writeMessage(base, post.Author, post.Title, postBody(post), postMsgID, "", 0, post.Created, post.URL())
		if err != nil {
			return err
		}
		known[postMsgID] = num
		res.Posts++
	}
	for _, c := range comments {
		id := msgIDFor(c.ID)
		if known[id] != 0 {
			continue
		}
		parentID := msgIDFor(c.ParentID)
		if known[parentID] == 0 {
			parentID = postMsgID // parent removed or not on the page
		}
		num, err := writeMessage(base, c.Author, "Re: "+post.Title, commentBody(c, post), id, parentID, known[parentID], c.Created, post.URL())
		if err != nil {
			return err
		}
		known[id] = num
		res.Comments++
	}
	return im.Store.Set(sub, p.ID, p.CommentCount)
}

func (im *Importer) dryRun(post Post, comments []Comment, known map[string]int, res *SubResult) {
	if known[msgIDFor(post.ID)] == 0 {
		res.Posts++
		_, _ = fmt.Fprintf(im.Out, "post    %s  u/%s  %s\n", post.ID, post.Author, post.Title)
	}
	for _, c := range comments {
		if known[msgIDFor(c.ID)] == 0 {
			res.Comments++
			_, _ = fmt.Fprintf(im.Out, "comment %s  u/%s  -> %s\n", c.ID, c.Author, c.ParentID)
		}
	}
}

func postBody(p Post) string {
	text := HTMLToText(p.BodyHTML)
	if text == "" && p.ContentHref != "" && p.ContentHref != p.URL() {
		text = p.ContentHref
	}
	return withPermalink(text, p.URL())
}

func commentBody(c Comment, p Post) string {
	return withPermalink(HTMLToText(c.BodyHTML), p.URL())
}

func withPermalink(text, url string) string {
	if text == "" {
		return url
	}
	return text + "\n\n" + url
}

func writeMessage(base *jam.Base, author, subject, body, msgID, replyID string, replyTo int, created time.Time, permalink string) (int, error) {
	jm := jam.NewMessage()
	jm.From = "u/" + author
	jm.To = message.MsgToUserAll
	jm.Subject = subject
	jm.Text = body
	jm.DateTime = created
	if jm.DateTime.IsZero() {
		jm.DateTime = time.Now()
	}
	jm.MsgID = msgID
	jm.ReplyID = replyID
	if replyTo > 0 {
		jm.ReplyTo = uint32(replyTo)
	}
	jm.Kludges = append(jm.Kludges, "CHRS: UTF-8 4", "REDDIT: "+permalink)
	return base.WriteMessage(jm)
}
