package qwknet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// PollResult reports one full exchange with the hub.
type PollResult struct {
	Scan       ScanResult
	Uploaded   bool
	Downloaded bool
	Bytes      int64 // size of the downloaded packet
	Toss       TossResult
	Errors     []string
}

// Poll runs the whole cycle: toss anything left from last time, scan new
// posts into the REP, upload it, download the hub's packet, toss that.
// Each step that fails is reported and the later steps still run where
// they can, so a hub outage never stops local posts from being packed and
// a packed REP never stops a download from being tossed.
func (n *Node) Poll(ctx context.Context) PollResult {
	var res PollResult

	if len(n.inboundPackets()) > 0 {
		res.Toss.add(n.Toss())
	}

	res.Scan = n.Scan()

	if err := n.exchange(ctx, &res); err != nil {
		res.Errors = append(res.Errors, err.Error())
	}

	if res.Downloaded || len(n.inboundPackets()) > 0 {
		res.Toss.add(n.Toss())
	}
	return res
}

// exchange is the FTP session: REP up, QWK down.
func (n *Node) exchange(ctx context.Context, res *PollResult) error {
	c, err := n.connect(ctx)
	if err != nil {
		return err
	}
	defer c.quit()

	n.uploadREP(ctx, c, res)

	size, path, err := n.download(ctx, c)
	if err != nil {
		return err
	}
	if path != "" {
		res.Downloaded = true
		res.Bytes = size
		slog.Info("qwknet QWK downloaded", "network", n.Key, "hub", n.hubID, "bytes", size, "path", path)
	} else {
		slog.Info("qwknet hub had no packet for us", "network", n.Key, "hub", n.hubID)
	}
	return nil
}

// uploadREP sends the waiting REP, if any, and retires it once the hub has
// it. It holds the REP lock throughout, so a Scan in another process cannot
// append messages to a REP that is about to be deleted as delivered. A
// failure is recorded and the caller goes on to download.
func (n *Node) uploadREP(ctx context.Context, c *ftpClient, res *PollResult) {
	repPath := n.repPath()
	lock, err := n.lockREP()
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("upload REP: %v", err))
		return
	}
	defer lock.Release()

	st, err := os.Stat(repPath)
	if err != nil || st.Size() == 0 {
		return // nothing waiting
	}
	f, err := os.Open(repPath)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("open REP: %v", err))
		return
	}
	uerr := c.store(ctx, n.hubID+".REP", f)
	_ = f.Close() // read-only
	if uerr != nil {
		// The REP stays in outbound for the next poll. The download still
		// runs: a hub refusing uploads must not also cut off incoming
		// mail. If the connection itself is gone, it fails at once.
		res.Errors = append(res.Errors, fmt.Sprintf("upload REP: %v", uerr))
		slog.Warn("qwknet REP upload failed; kept for the next poll", "network", n.Key, "hub", n.hubID, "error", uerr)
		return
	}
	res.Uploaded = true
	if err := retireUploadedREP(repPath); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("uploaded REP left in outbound and will be sent again: %v", err))
	}
	slog.Info("qwknet REP uploaded", "network", n.Key, "hub", n.hubID, "bytes", st.Size())
}

// maxPacketDownload caps a packet download. The hub is reached over plain
// FTP, so a bad hub or anyone on the path could otherwise stream until the
// inbound disk fills; the zip limits in package qwk only apply once the
// file is opened. Far above any real packet. A variable so tests can lower
// it.
var maxPacketDownload int64 = 512 << 20

// errPacketTooLarge stops a download that passes maxPacketDownload.
var errPacketTooLarge = errors.New("packet exceeds the download size limit")

// cappedWriter passes writes through until left bytes have gone, then
// fails, so io.Copy stops without writing past the limit.
type cappedWriter struct {
	w    io.Writer
	left int64
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > c.left {
		return 0, fmt.Errorf("%w (%d bytes)", errPacketTooLarge, maxPacketDownload)
	}
	n, err := c.w.Write(p)
	c.left -= int64(n)
	return n, err
}

// retireUploadedREP gets a delivered REP out of the way of the next Scan,
// which would otherwise read its messages back as still pending and upload
// them a second time. Removal is tried first; failing that the file is
// renamed aside, and failing that emptied, since Scan and exchange both
// treat an empty REP as nothing waiting. Only when all three fail does the
// REP stay live, and the error says so.
func retireUploadedREP(repPath string) error {
	rmErr := os.Remove(repPath)
	if rmErr == nil || os.IsNotExist(rmErr) {
		return nil
	}
	sent := repPath + ".sent"
	if err := atomicfile.Replace(repPath, sent); err == nil {
		slog.Warn("could not remove uploaded REP; renamed it aside", "path", repPath, "moved_to", sent, "error", rmErr)
		return nil
	}
	if err := os.Truncate(repPath, 0); err == nil {
		slog.Warn("could not remove or rename uploaded REP; emptied it", "path", repPath, "error", rmErr)
		return nil
	}
	return rmErr
}

// connect dials and logs in to the hub.
func (n *Node) connect(ctx context.Context) (*ftpClient, error) {
	timeout := time.Duration(n.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	c, err := ftpDial(ctx, n.cfg.HostPort(), timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to hub %s: %w", n.cfg.HostPort(), err)
	}
	if err := c.login(n.cfg.LoginUser(n.nodeID), n.cfg.Password); err != nil {
		c.quit()
		return nil, fmt.Errorf("hub %s: %w", n.cfg.HostPort(), err)
	}
	return c, nil
}

// download fetches <HUBID>.QWK into the inbound directory. It returns ""
// for the path when the hub had nothing (550, or an empty file, which some
// hubs send instead).
//
// The partial file is written in the inbound directory itself, so the final
// rename never crosses a filesystem, and under a unique name inboundPackets
// ignores. Once RETR completes the hub counts the packet as delivered and
// will not send it again, so from then on a failure keeps the file rather
// than deleting the only copy.
func (n *Node) download(ctx context.Context, c *ftpClient) (int64, string, error) {
	f, err := os.CreateTemp(n.paths.InboundPath, n.hubID+".QWK.*.part")
	if err != nil {
		return 0, "", fmt.Errorf("create temp download: %w", err)
	}
	tmp := f.Name()
	size, rerr := c.retrieve(ctx, n.hubID+".QWK", &cappedWriter{w: f, left: maxPacketDownload})
	cerr := f.Close()
	if rerr != nil {
		_ = os.Remove(tmp)
		if rerr == errNoSuchFile {
			return 0, "", nil
		}
		return 0, "", fmt.Errorf("download QWK: %w", rerr)
	}
	if cerr != nil {
		return 0, "", fmt.Errorf("finish download (packet kept at %s): %w", tmp, cerr)
	}
	if size == 0 {
		_ = os.Remove(tmp)
		return 0, "", nil
	}
	dest := n.newInboundName()
	if err := os.Rename(tmp, dest); err != nil {
		return 0, "", fmt.Errorf("move download into inbound (packet kept at %s; rename it to %s to toss it): %w",
			tmp, filepath.Base(dest), err)
	}
	return size, dest, nil
}

// FetchConferences returns the hub's conference list, for choosing which
// to mirror. A packet already waiting in the inbound directory answers
// without a connection; otherwise one is downloaded and left there for the
// next toss, since the hub will not send those messages again.
func (n *Node) FetchConferences(ctx context.Context) ([]qwk.ConferenceInfo, error) {
	if waiting := n.inboundPackets(); len(waiting) > 0 {
		if confs, err := readConferences(waiting[len(waiting)-1]); err == nil && len(confs) > 0 {
			return confs, nil
		}
	}
	c, err := n.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.quit()
	_, path, err := n.download(ctx, c)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("hub %s sent no packet, so its conference list could not be read; try again later", n.hubID)
	}
	return readConferences(path)
}

func readConferences(path string) ([]qwk.ConferenceInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	p, err := qwk.ReadPacket(f, st.Size())
	if err != nil {
		return nil, err
	}
	return p.Conferences, nil
}
