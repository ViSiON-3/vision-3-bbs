package hub

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The hub keeps the coordinator handovers it accepted for each network
// (see protocol.CoordHandover) and serves the most recent ones with the NAL.
// A handover is accepted from any subscriber, since its signature is the
// authority: it must be signed by the coordinator key in force, which is the
// stored NAL's key followed along the handovers already held. From then on
// the hub takes NALs signed by the new key, and a hub restarted with the new
// key re-signs the stored NAL with it (adoptHandovers), so leaves move their
// pins at their next fetch. A hub already running the new key when the
// handover is posted re-signs the NAL as soon as it accepts it.
//
// A hub restarted with a new key and no handover may also have re-signed the
// NAL with that key before the handover arrives (merging initialAreas at
// startup, approving a proposal). It then records the key that signed the
// NAL before, a previous signer, and still takes a chain of handovers from
// a recorded key to its own (see handlePostHandover). It keeps the last
// maxPrevSigners such keys, so a hub switched twice or more before anyone
// posted a handover can still be bridged.

// maxPrevSigners is how many previous signers the hub keeps per network.
// A chain from the oldest of them to the hub's key then has at most
// nal.MaxHandoverSteps links, the longest a leaf follows.
const maxPrevSigners = nal.MaxHandoverSteps

const handoverSchema = `
CREATE TABLE IF NOT EXISTS coord_handovers (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	network       TEXT NOT NULL,
	handover_json TEXT NOT NULL,
	received_at   DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_coord_handovers_network ON coord_handovers(network, id);
CREATE TABLE IF NOT EXISTS coord_signer_history (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	network     TEXT NOT NULL,
	key_b64     TEXT NOT NULL,
	recorded_at DATETIME DEFAULT (datetime('now')),
	UNIQUE (network, key_b64)
);
`

// migratePrevSigners moves the one previous signer per network that
// coord_prev_signers held (before the history) into coord_signer_history,
// where it becomes the network's only entry, and drops the old table. A
// database without the old table is left alone.
func migratePrevSigners(db *sql.DB) error {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'coord_prev_signers'`).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hub: migrate previous signers: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("hub: migrate previous signers: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO coord_signer_history (network, key_b64, recorded_at)
		SELECT network, key_b64, recorded_at FROM coord_prev_signers ORDER BY recorded_at`); err != nil {
		return fmt.Errorf("hub: migrate previous signers: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE coord_prev_signers`); err != nil {
		return fmt.Errorf("hub: migrate previous signers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("hub: migrate previous signers: %w", err)
	}
	return nil
}

// PrevSigners returns the keys that signed network's NAL before the hub
// re-signed it with its own key without a handover, oldest first, or none
// if it never has.
func (ns *NALStore) PrevSigners(network string) ([]string, error) {
	rows, err := ns.db.Query("SELECT key_b64 FROM coord_signer_history WHERE network = ? ORDER BY id", network)
	if err != nil {
		return nil, fmt.Errorf("hub: list previous signers: %w", err)
	}
	defer func() { _ = rows.Close() }() // read side
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("hub: scan previous signer: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hub: list previous signers: %w", err)
	}
	return keys, nil
}

// AddPrevSigner records key as network's most recent previous signer. A key
// already recorded moves to the end, and only the newest maxPrevSigners are
// kept.
func (ns *NALStore) AddPrevSigner(network, key string) error {
	tx, err := ns.db.Begin()
	if err != nil {
		return fmt.Errorf("hub: add previous signer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM coord_signer_history WHERE network = ? AND key_b64 = ?", network, key); err != nil {
		return fmt.Errorf("hub: add previous signer: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO coord_signer_history (network, key_b64) VALUES (?, ?)", network, key); err != nil {
		return fmt.Errorf("hub: add previous signer: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM coord_signer_history WHERE network = ? AND id NOT IN (
		SELECT id FROM coord_signer_history WHERE network = ? ORDER BY id DESC LIMIT ?)`,
		network, network, maxPrevSigners); err != nil {
		return fmt.Errorf("hub: add previous signer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("hub: add previous signer: %w", err)
	}
	return nil
}

// Handovers returns the handovers stored for network, oldest first.
func (ns *NALStore) Handovers(network string) ([]protocol.CoordHandover, error) {
	rows, err := ns.db.Query("SELECT handover_json FROM coord_handovers WHERE network = ? ORDER BY id", network)
	if err != nil {
		return nil, fmt.Errorf("hub: list handovers: %w", err)
	}
	defer func() { _ = rows.Close() }() // read side
	var hs []protocol.CoordHandover
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("hub: scan handover: %w", err)
		}
		var h protocol.CoordHandover
		if err := json.Unmarshal([]byte(data), &h); err != nil {
			return nil, fmt.Errorf("hub: unmarshal handover: %w", err)
		}
		hs = append(hs, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hub: list handovers: %w", err)
	}
	return hs, nil
}

// AddHandover stores a handover for network after the others. It does not
// check it; the caller has.
func (ns *NALStore) AddHandover(network string, h *protocol.CoordHandover) error {
	return ns.AddHandovers(network, []protocol.CoordHandover{*h})
}

// AddHandovers stores hs for network after the others, in order, all or
// none. It does not check them; the caller has.
func (ns *NALStore) AddHandovers(network string, hs []protocol.CoordHandover) error {
	tx, err := ns.db.Begin()
	if err != nil {
		return fmt.Errorf("hub: add handover: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := addHandoversTx(tx, network, hs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("hub: add handover: %w", err)
	}
	return nil
}

// commitHandovers stores, in one transaction, the NAL n for network unless
// it is nil, then the handovers hs after the others, and clears network's
// previous signers: the update that re-signs the NAL for a key it was
// handed over to and moves the retired keys' areas (see adoptHandover).
func (ns *NALStore) commitHandovers(network string, n *protocol.NAL, hs []protocol.CoordHandover) error {
	tx, err := ns.db.Begin()
	if err != nil {
		return fmt.Errorf("hub: store handover update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if n != nil {
		if err := putTx(tx, network, n); err != nil {
			return err
		}
	}
	if err := addHandoversTx(tx, network, hs); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM coord_signer_history WHERE network = ?", network); err != nil {
		return fmt.Errorf("hub: clear previous signers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("hub: store handover update: %w", err)
	}
	return nil
}

// addHandoversTx stores hs for network within tx.
func addHandoversTx(tx *sql.Tx, network string, hs []protocol.CoordHandover) error {
	for i := range hs {
		data, err := json.Marshal(&hs[i])
		if err != nil {
			return fmt.Errorf("hub: marshal handover: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO coord_handovers (network, handover_json) VALUES (?, ?)", network, string(data)); err != nil {
			return fmt.Errorf("hub: add handover: %w", err)
		}
	}
	return nil
}

// coordKeyInForce returns the coordinator key in force for network, from NAL
// n's key along hs (see nal.CoordKeyPath).
func coordKeyInForce(n *protocol.NAL, hs []protocol.CoordHandover) string {
	path := coordKeyPath(n, hs)
	return path[len(path)-1]
}

// coordKeyPath returns the keys coordKeyInForce passes through: n's key
// first, then each key hs hands it on to, ending with the key in force.
func coordKeyPath(n *protocol.NAL, hs []protocol.CoordHandover) []string {
	return nal.CoordKeyPath(n.CoordPubKeyB64, hs)
}

// servedHandovers trims hs to the most recent nal.MaxHandovers, the most a
// leaf looks at.
func servedHandovers(hs []protocol.CoordHandover) []protocol.CoordHandover {
	if len(hs) > nal.MaxHandovers {
		return hs[len(hs)-nal.MaxHandovers:]
	}
	return hs
}

// nodeIDOf returns the node ID of a base64 public key, or "" if it is not
// one.
func nodeIDOf(keyB64 string) string {
	raw, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return ""
	}
	return keystore.NodeIDFromPubKey(raw)
}

// handlePostHandover accepts a coordinator handover for a network (auth
// required, from any subscriber). It must verify, name the network, be
// signed by the coordinator key in force and hand over to a key the
// network's chain has not used, so the stored handovers never loop. Posting
// one the hub already holds (the same old and new key) succeeds without
// storing it twice. When the handover hands the network to the hub's own
// key, the reply is 200 only once the NAL is signed by that key.
//
// The handover may also come late: the hub was restarted with the new key
// before it was posted. The stored NAL is then still the old key's, and with
// no handover leading on from it that key is the one in force, so the usual
// rules accept the handover; the hub then re-signs the NAL with its key
// straight away, as adoptHandovers would at the next restart. This is no
// weaker than the usual order: the handover is still checked against the key
// in force, which only that key's holder can sign for. The hub's own key
// only ever narrows what is accepted, never widens it, so running a key
// does not let a hub take the network for that key or any other. To keep
// a switched hub from taking a handover it can never act on, while the hub
// runs a key that neither signs the stored NAL nor appears on the chain,
// the only handover it accepts is one to its own key.
//
// If the switched hub has already re-signed the NAL with its key, the old
// key is no longer in force; the handover is then accepted from one of the
// network's previous signers instead (see nal.CheckPostedChain).
//
// The body is one handover, or an array of them that forms a chain, each
// starting at the key the one before handed over to. A chain is checked
// link by link, as if each were posted after the one before, or as a whole
// against the previous signers, and stored whole or not at all. It is how a
// hub switched more than once without a handover (A to B to C) is bridged:
// posted one at a time, B to C would be held first and A's link then
// refused, since a held handover ends the previous signers' say. Links the
// hub already holds are repeats and are skipped; a chain of nothing else is
// a repeat.
func (h *Hub) handlePostHandover(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}

	chain, status, msg := decodeHandovers(r.Body, network)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}

	h.nalMu.Lock()
	defer h.nalMu.Unlock()

	n, err := h.nalStore.Get(network)
	if err != nil {
		slog.Error("get nal for handover", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if n == nil {
		http.Error(w, `{"error":"no NAL available"}`, http.StatusNotFound)
		return
	}
	hs, err := h.nalStore.Handovers(network)
	if err != nil {
		slog.Error("list handovers", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	hubKey := h.hubKey()
	prev, err := h.nalStore.PrevSigners(network)
	if err != nil {
		slog.Error("list previous signers", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	check, err := nal.CheckPostedChain(n.CoordPubKeyB64, hs, chain, prev, hubKey)
	if err != nil {
		http.Error(w, refusalBody(err), http.StatusConflict)
		return
	}
	// The same handover, or the same old key's statement re-signed at
	// another time (as v3net-handover does when run again), is a repeat:
	// it is not stored again, but re-signing is retried, since it may have
	// failed when the handover was first posted.
	fresh, fromPrev := check.Fresh, check.FromPrevSigners
	if len(fresh) == 0 {
		h.answerAdopted(w, network)
		return
	}

	// A chain to a NAL already the hub key's is stored with the NAL, its
	// old keys' areas moved to the hub, in one update; one to a NAL that
	// is not is adopted below like any handover to the hub's key.
	if fromPrev && n.CoordPubKeyB64 == hubKey {
		changed := n
		if h.moveManagedAreas(n, oldKeys(fresh)) {
			if err := nal.Sign(n, h.cfg.Keystore); err != nil {
				slog.Error("hub: could not re-sign the NAL for the previous signers' areas", "network", network, "error", err)
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
		} else {
			changed = nil
		}
		if err := h.nalStore.commitHandovers(network, changed, fresh); err != nil {
			slog.Error("add handover", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		logAccepted(network, fresh)
		h.publishNALUpdated(network, n)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if err := h.nalStore.AddHandovers(network, fresh); err != nil {
		slog.Error("add handover", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	logAccepted(network, fresh)
	h.answerAdopted(w, network)
}

// handleGetSigners serves what the hub records about the keys that signed
// network's NAL (auth required, from any subscriber), so v3net-handover can
// tell the operator which chain the hub needs. It holds public keys only. It
// sits behind authMiddleware so signResponses binds the reply to the request:
// the tool acts on it only once that signature verifies.
func (h *Hub) handleGetSigners(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}

	h.nalMu.Lock()
	defer h.nalMu.Unlock()

	n, err := h.nalStore.Get(network)
	if err != nil {
		slog.Error("get nal for signer history", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if n == nil {
		http.Error(w, `{"error":"no NAL available"}`, http.StatusNotFound)
		return
	}
	hs, err := h.nalStore.Handovers(network)
	if err != nil {
		slog.Error("list handovers", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	prev, err := h.nalStore.PrevSigners(network)
	if err != nil {
		slog.Error("list previous signers", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	history := protocol.SignerHistory{
		Network:       network,
		NALKeyB64:     n.CoordPubKeyB64,
		KeyInForceB64: coordKeyInForce(n, hs),
		PrevSigners:   append([]string{}, prev...),
		Handovers:     make([]protocol.HandoverLink, 0, len(hs)),
	}
	for _, ho := range hs {
		history.Handovers = append(history.Handovers, protocol.HandoverLink{OldKeyB64: ho.OldKeyB64, NewKeyB64: ho.NewKeyB64})
	}
	writeJSON(w, http.StatusOK, history)
}

// refusalBody returns the JSON body for a refused handover post.
func refusalBody(err error) string {
	return fmt.Sprintf(`{"error":%q}`, err.Error())
}

// logAccepted logs each handover of network the hub just stored.
func logAccepted(network string, hs []protocol.CoordHandover) {
	for _, ho := range hs {
		slog.Info("hub: coordinator handover accepted", "network", network, "from", ho.OldKeyB64, "to", ho.NewKeyB64)
	}
}

// keysHandedOverFrom returns the keys whose areas move when the stored NAL n
// is re-signed for the key its handovers hs lead to: the keys on that chain,
// and the previous signers prev it bridged to n's key.
func keysHandedOverFrom(n *protocol.NAL, hs []protocol.CoordHandover, prev []string) []string {
	path := coordKeyPath(n, hs)
	return append(path[:len(path)-1:len(path)-1], retiredSigners(n, hs, prev)...)
}

// retiredSigners returns the previous signers prev that the handovers hs
// lead from to the key of the stored NAL n: keys a late chain bridged
// before the hub re-signed n for its own key. Their areas never moved, as
// a re-sign without a handover moves nothing.
func retiredSigners(n *protocol.NAL, hs []protocol.CoordHandover, prev []string) []string {
	if len(prev) == 0 {
		return nil
	}
	reached := map[string]bool{n.CoordPubKeyB64: true}
	for grew := true; grew; {
		grew = false
		for _, h := range hs {
			if reached[h.NewKeyB64] && !reached[h.OldKeyB64] {
				reached[h.OldKeyB64] = true
				grew = true
			}
		}
	}
	var keys []string
	for _, key := range prev {
		if key != n.CoordPubKeyB64 && reached[key] {
			keys = append(keys, key)
		}
	}
	return keys
}

// decodeHandovers reads a handover post for network: one handover, or an
// array that nal.CheckChainShape accepts. Each must name network and verify. It returns the handovers, or the
// status and body to refuse the post with.
func decodeHandovers(body io.Reader, network string) ([]protocol.CoordHandover, int, string) {
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, http.StatusBadRequest, `{"error":"invalid JSON"}`
	}
	var chain []protocol.CoordHandover
	if trimmed := bytes.TrimLeft(raw, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(raw, &chain); err != nil {
			return nil, http.StatusBadRequest, `{"error":"invalid JSON"}`
		}
	} else {
		var ho protocol.CoordHandover
		if err := json.Unmarshal(raw, &ho); err != nil {
			return nil, http.StatusBadRequest, `{"error":"invalid JSON"}`
		}
		chain = []protocol.CoordHandover{ho}
	}
	if err := nal.CheckChainShape(chain); err != nil {
		return nil, http.StatusBadRequest, refusalBody(err)
	}
	for i := range chain {
		ho := &chain[i]
		if ho.Network != network {
			return nil, http.StatusBadRequest, `{"error":"network mismatch"}`
		}
		if err := ho.Verify(); err != nil {
			return nil, http.StatusUnprocessableEntity, `{"error":"handover verification failed"}`
		}
	}
	return chain, 0, ""
}

// oldKeys returns the key each of chain hands over from.
func oldKeys(chain []protocol.CoordHandover) []string {
	keys := make([]string, 0, len(chain))
	for _, ho := range chain {
		keys = append(keys, ho.OldKeyB64)
	}
	return keys
}

// moveManagedAreas makes the hub's node the manager of each area of n
// managed by the node of a key in from, the coordinator keys the network
// was handed over from, and logs each move. Areas other nodes manage are
// left alone. It changes n in place and reports whether any area moved;
// the caller signs and stores n, so the moves and the re-sign are one NAL
// update. Without it the old coordinator's areas would keep a manager
// nobody runs any more until the operator reassigned each one.
func (h *Hub) moveManagedAreas(n *protocol.NAL, from []string) bool {
	hubKey, hubNode := h.hubKey(), h.cfg.Keystore.NodeID()
	old := make(map[string]bool, 2*len(from))
	for _, key := range from {
		if key == "" || key == hubKey {
			continue
		}
		old[key] = true
		if id := nodeIDOf(key); id != "" {
			old[id] = true
		}
	}
	moved := false
	for i := range n.Areas {
		a := &n.Areas[i]
		if !old[a.ManagerPubKeyB64] && !old[a.ManagerNodeID] {
			continue
		}
		slog.Info("hub: area manager moved to the hub after a coordinator handover",
			"network", n.Network, "tag", a.Tag, "from", a.ManagerNodeID, "to", hubNode)
		a.ManagerNodeID, a.ManagerPubKeyB64 = hubNode, hubKey
		moved = true
	}
	return moved
}

// errAdoptFailed is the reply to a handover the hub holds but could not
// re-sign the NAL for.
const errAdoptFailed = `{"error":"handover held, but re-signing the NAL with the hub key failed (see the hub log); post the handover again to retry"}`

// answerAdopted re-signs network's NAL if a held handover hands it to the
// hub's key (adoptPostedHandover) and answers the post: 200 when that
// worked or was not needed, 500 with errAdoptFailed when it failed, so the
// poster does not report a recovery that has not happened.
func (h *Hub) answerAdopted(w http.ResponseWriter, network string) {
	if err := h.adoptPostedHandover(network); err != nil {
		http.Error(w, errAdoptFailed, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// hubKey returns the hub's public key, or "" when it runs without one.
func (h *Hub) hubKey() string {
	if h.cfg.Keystore == nil {
		return ""
	}
	return h.cfg.Keystore.PubKeyBase64()
}

// adoptPostedHandover re-signs network's NAL with the hub's key when a
// handover just posted hands the network to it (see adoptHandover), and
// tells subscribers the NAL changed so they fetch it, and move their pins,
// now. A failure is logged and returned; the handover stays, and posting it
// again or the next restart retries. Call it with nalMu held.
func (h *Hub) adoptPostedHandover(network string) error {
	n, err := h.adoptHandover(network)
	if err != nil {
		slog.Error("hub: could not re-sign the NAL after a handover", "network", network, "error", err)
		return err
	}
	if n != nil {
		h.publishNALUpdated(network, n)
	}
	return nil
}

// publishNALUpdated tells network's subscribers that its NAL, now n,
// changed, so they fetch it.
func (h *Hub) publishNALUpdated(network string, n *protocol.NAL) {
	ev, err := protocol.NewEvent(protocol.EventNALUpdated, protocol.NALUpdatedPayload{
		Network:   network,
		Updated:   n.Updated,
		AreaCount: len(n.Areas),
	})
	if err != nil {
		slog.Error("build nal_updated event", "network", network, "error", err)
		return
	}
	h.broadcaster.Publish(network, ev)
}

// ErrHandedOver is returned by SignNAL, and so by every hub-side NAL change,
// when the network was handed over to a key other than the one the hub
// runs with. Signing with the hub key would undo the handover for leaves
// that already follow the new key, so the change is refused until the hub
// runs with the key in force.
var ErrHandedOver = errors.New("hub: the network was handed over to another coordinator key; restart the hub with that key to change the NAL")

// SignNAL signs n with the hub's key, unless n's network has handovers and
// the coordinator key in force is not the hub's, when it returns
// ErrHandedOver. A network never handed over is signed as before, whatever
// key signed its stored NAL; when that was another key, it is first added
// to the network's previous signers, so a late chain of handovers from it
// is still accepted. When n's network was handed to the hub's key but the
// stored NAL is still another key's, because the re-sign failed, this change
// is that re-sign: the areas it would have moved move in n (see
// adoptHandover), and storing n drops the previous signers (see putTx), so no
// later re-sign moves them again. Every hub-side NAL change signs through it;
// call it with nalMu held, or before the hub serves requests.
func (h *Hub) SignNAL(n *protocol.NAL) error {
	stored, err := h.nalStore.Get(n.Network)
	if err != nil {
		return err
	}
	if stored != nil {
		hs, err := h.nalStore.Handovers(n.Network)
		if err != nil {
			return err
		}
		hubKey := h.cfg.Keystore.PubKeyBase64()
		if len(hs) > 0 && coordKeyInForce(stored, hs) != hubKey {
			return ErrHandedOver
		}
		// Recorded before signing: if the change is then not stored, the
		// record names the key still signing the NAL, which the
		// previous-signer rule ignores (it needs the NAL to be the hub's)
		// until a later change stores it and records the key again.
		if len(hs) == 0 && stored.CoordPubKeyB64 != hubKey {
			if err := h.nalStore.AddPrevSigner(n.Network, stored.CoordPubKeyB64); err != nil {
				return err
			}
		}
		if len(hs) > 0 && stored.CoordPubKeyB64 != hubKey {
			prev, err := h.nalStore.PrevSigners(n.Network)
			if err != nil {
				return err
			}
			h.moveManagedAreas(n, keysHandedOverFrom(stored, hs, prev))
		}
	}
	return nal.Sign(n, h.cfg.Keystore)
}

// refuseHandedOver answers a NAL change that SignNAL refused with 409 and
// reports whether it did.
func refuseHandedOver(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, ErrHandedOver) {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": ErrHandedOver.Error()})
	return true
}

// adoptHandovers re-signs each network's stored NAL with the hub's key when
// a handover chain from the NAL's key ends at it: the hub has been
// restarted with the key the network was handed to. Leaves then find the
// new key at their next fetch, without waiting for an area change. Failures
// are logged; the old NAL stays until the next change re-signs it.
func (h *Hub) adoptHandovers() {
	if h.cfg.Keystore == nil {
		return
	}
	h.nalMu.Lock()
	defer h.nalMu.Unlock()
	for _, nc := range h.cfg.Networks {
		if _, err := h.adoptHandover(nc.Name); err != nil {
			slog.Error("hub: could not re-sign the NAL after a handover", "network", nc.Name, "error", err)
		}
	}
}

// adoptHandover re-signs network's stored NAL with the hub's key when a
// handover chain from the NAL's key ends at it, and returns the re-signed
// NAL, or nil when there was nothing to do. Areas managed by the nodes of
// the keys the network is handed over from move to the hub's node in the
// same update (see moveManagedAreas): the keys on that chain, and the
// previous signers a late chain bridged to the NAL's key (see
// retiredSigners). Both come from what the hub stores, so a re-sign that
// failed, or never ran before a restart, moves the same areas when it is
// retried. The update also clears the previous signers, whose areas have
// then moved, so a later re-sign never moves them again over a manager the
// operator has since chosen. Call it with nalMu held, or before the hub
// serves requests.
func (h *Hub) adoptHandover(network string) (*protocol.NAL, error) {
	hubKey := h.hubKey()
	if hubKey == "" {
		return nil, nil
	}
	n, err := h.nalStore.Get(network)
	if err != nil || n == nil || n.CoordPubKeyB64 == hubKey {
		return nil, err
	}
	hs, err := h.nalStore.Handovers(network)
	if err != nil {
		return nil, err
	}
	path := coordKeyPath(n, hs)
	if path[len(path)-1] != hubKey {
		return nil, nil
	}
	prev, err := h.nalStore.PrevSigners(network)
	if err != nil {
		return nil, err
	}
	from := n.CoordPubKeyB64
	h.moveManagedAreas(n, keysHandedOverFrom(n, hs, prev))
	if err := nal.Sign(n, h.cfg.Keystore); err != nil {
		return nil, err
	}
	if err := h.nalStore.commitHandovers(network, n, nil); err != nil {
		return nil, err
	}
	slog.Info("hub: NAL re-signed with the key the network was handed over to",
		"network", network, "from", from, "to", hubKey)
	return n, nil
}
