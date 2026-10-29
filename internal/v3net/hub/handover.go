package hub

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
// NAL before, the previous signer, and still takes that key's handover to
// its own (see handlePostHandover).

const handoverSchema = `
CREATE TABLE IF NOT EXISTS coord_handovers (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	network       TEXT NOT NULL,
	handover_json TEXT NOT NULL,
	received_at   DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_coord_handovers_network ON coord_handovers(network, id);
CREATE TABLE IF NOT EXISTS coord_prev_signers (
	network     TEXT PRIMARY KEY,
	key_b64     TEXT NOT NULL,
	recorded_at DATETIME DEFAULT (datetime('now'))
);
`

// PrevSigner returns the key that signed network's NAL before the hub last
// re-signed it with its own key without a handover, or "" if it never has.
func (ns *NALStore) PrevSigner(network string) (string, error) {
	var key string
	err := ns.db.QueryRow("SELECT key_b64 FROM coord_prev_signers WHERE network = ?", network).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("hub: get previous signer: %w", err)
	}
	return key, nil
}

// SetPrevSigner records key as network's previous signer, replacing any
// recorded before.
func (ns *NALStore) SetPrevSigner(network, key string) error {
	if _, err := ns.db.Exec(
		`INSERT INTO coord_prev_signers (network, key_b64) VALUES (?, ?)
		 ON CONFLICT(network) DO UPDATE SET key_b64 = excluded.key_b64, recorded_at = datetime('now')`,
		network, key,
	); err != nil {
		return fmt.Errorf("hub: set previous signer: %w", err)
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
	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("hub: marshal handover: %w", err)
	}
	if _, err := ns.db.Exec("INSERT INTO coord_handovers (network, handover_json) VALUES (?, ?)", network, string(data)); err != nil {
		return fmt.Errorf("hub: add handover: %w", err)
	}
	return nil
}

// coordKeyInForce returns the coordinator key in force for network: the key
// of NAL n, followed along hs for as long as one of them hands it on. It is
// n's own key when no handover starts there, so a NAL re-signed by a key off
// the chain is not overruled by older handovers.
func coordKeyInForce(n *protocol.NAL, hs []protocol.CoordHandover) string {
	path := coordKeyPath(n, hs)
	return path[len(path)-1]
}

// coordKeyPath returns the keys coordKeyInForce passes through: n's key
// first, then each key hs hands it on to, ending with the key in force.
func coordKeyPath(n *protocol.NAL, hs []protocol.CoordHandover) []string {
	key := n.CoordPubKeyB64
	path := []string{key}
	seen := map[string]bool{key: true}
	for moved := true; moved; {
		moved = false
		for _, h := range hs {
			if h.OldKeyB64 == key && !seen[h.NewKeyB64] {
				key = h.NewKeyB64
				seen[key] = true
				path = append(path, key)
				moved = true
				break
			}
		}
	}
	return path
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
// key is no longer in force; the handover is then accepted from the
// network's previous signer instead (see fromPrevSigner).
func (h *Hub) handlePostHandover(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}

	var ho protocol.CoordHandover
	if err := json.NewDecoder(r.Body).Decode(&ho); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if ho.Network != network {
		http.Error(w, `{"error":"network mismatch"}`, http.StatusBadRequest)
		return
	}
	if err := ho.Verify(); err != nil {
		http.Error(w, `{"error":"handover verification failed"}`, http.StatusUnprocessableEntity)
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
	for _, old := range hs {
		// The same handover, or the same old key's statement re-signed at
		// another time (as v3net-handover does when run again), is a
		// repeat: it is not stored again, but re-signing is retried, since
		// it may have failed when the handover was first posted.
		if old.OldKeyB64 == ho.OldKeyB64 && old.NewKeyB64 == ho.NewKeyB64 {
			h.answerAdopted(w, network)
			return
		}
	}
	hubKey := h.hubKey()
	fromPrev, err := h.fromPrevSigner(network, n, hs, &ho, hubKey)
	if err != nil {
		slog.Error("get previous signer", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if !fromPrev {
		if ho.OldKeyB64 != coordKeyInForce(n, hs) {
			http.Error(w, `{"error":"handover is not signed by the coordinator key in force"}`, http.StatusConflict)
			return
		}
		if ho.NewKeyB64 == n.CoordPubKeyB64 || handoverUsesKey(hs, ho.NewKeyB64) {
			http.Error(w, `{"error":"handover returns to a key the network already used"}`, http.StatusConflict)
			return
		}
		if hubSwitched(n, hs, hubKey) && ho.NewKeyB64 != hubKey {
			http.Error(w, `{"error":"the hub already runs a new key; only a handover to the hub's key is accepted"}`, http.StatusConflict)
			return
		}
	}

	if fromPrev {
		if err := h.moveAreasToHub(network, n, ho.OldKeyB64); err != nil {
			slog.Error("hub: could not move the previous signer's areas to the hub", "network", network, "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
	}
	if err := h.nalStore.AddHandover(network, &ho); err != nil {
		slog.Error("add handover", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("hub: coordinator handover accepted", "network", network, "from", ho.OldKeyB64, "to", ho.NewKeyB64)
	if fromPrev {
		// The NAL is already the hub key's, and its areas were moved to the
		// hub's node above; leaves only need the handover.
		h.publishNALUpdated(network, n)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	h.answerAdopted(w, network)
}

// moveAreasToHub makes the hub's node the manager of each area of NAL n
// that the previous signer's node still manages, then signs and stores n.
// It runs before a late handover from that key is stored (see
// fromPrevSigner), so a failure leaves nothing held and posting the
// handover again retries all of it. When no area moves, n is left as it
// is.
func (h *Hub) moveAreasToHub(network string, n *protocol.NAL, from string) error {
	if !h.moveManagedAreas(n, []string{from}) {
		return nil
	}
	if err := nal.Sign(n, h.cfg.Keystore); err != nil {
		return err
	}
	return h.nalStore.Put(network, n)
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

// fromPrevSigner reports whether ho is a late handover to a NAL the hub has
// already re-signed with its own key: no handover is held, the stored NAL n
// and ho's new key are both the hub's key, and ho's old key is the
// network's recorded previous signer (see SignNAL). ho has been verified,
// so the previous signer signed it. The chain is then just ho, from the key
// leaves still pin to the key that signs the NAL, and the NAL is re-signed
// only to move the previous signer's areas to the hub (see moveAreasToHub).
//
// This is as strong as the key-in-force rule: the previous signer was the
// key in force until the hub re-signed without a handover, and only its
// holder can sign ho. It cannot hand over to anything but the hub's own key,
// which already signs the NAL, and once any handover is held it no longer
// applies. If the previous signer was itself a lost key, nobody can sign a
// handover from it and the record is never used; its leaves re-pin by hand,
// as before. A stolen one gains its thief nothing: it can only point leaves
// at the key the hub already runs. Only one key back is recorded, so after
// two key changes without a handover only the latest can be bridged.
func (h *Hub) fromPrevSigner(network string, n *protocol.NAL, hs []protocol.CoordHandover, ho *protocol.CoordHandover, hubKey string) (bool, error) {
	if len(hs) > 0 || hubKey == "" || n.CoordPubKeyB64 != hubKey || ho.NewKeyB64 != hubKey {
		return false, nil
	}
	prev, err := h.nalStore.PrevSigner(network)
	if err != nil {
		return false, err
	}
	return prev != "" && ho.OldKeyB64 == prev, nil
}

// hubKey returns the hub's public key, or "" when it runs without one.
func (h *Hub) hubKey() string {
	if h.cfg.Keystore == nil {
		return ""
	}
	return h.cfg.Keystore.PubKeyBase64()
}

// hubSwitched reports whether the hub runs a key that neither signs the
// stored NAL n nor appears on its handovers hs: it was restarted with a new
// key before the network was handed to it.
func hubSwitched(n *protocol.NAL, hs []protocol.CoordHandover, hubKey string) bool {
	return hubKey != "" && hubKey != n.CoordPubKeyB64 && !handoverUsesKey(hs, hubKey)
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
// key signed its stored NAL; when that was another key, it is first
// recorded as the network's previous signer, so a late handover from it is
// still accepted. Every hub-side NAL change signs through it; call it with
// nalMu held, or before the hub serves requests.
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
		// previous-signer rule ignores (it needs the NAL to be the hub's).
		if len(hs) == 0 && stored.CoordPubKeyB64 != hubKey {
			if err := h.nalStore.SetPrevSigner(n.Network, stored.CoordPubKeyB64); err != nil {
				return err
			}
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

// handoverUsesKey reports whether key is either end of any of hs.
func handoverUsesKey(hs []protocol.CoordHandover, key string) bool {
	for _, h := range hs {
		if h.OldKeyB64 == key || h.NewKeyB64 == key {
			return true
		}
	}
	return false
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
// the keys on that chain move to the hub's node in the same update (see
// moveManagedAreas). Call it with nalMu held, or before the hub serves
// requests.
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
	from := n.CoordPubKeyB64
	h.moveManagedAreas(n, path[:len(path)-1])
	if err := nal.Sign(n, h.cfg.Keystore); err != nil {
		return nil, err
	}
	if err := h.nalStore.Put(network, n); err != nil {
		return nil, err
	}
	slog.Info("hub: NAL re-signed with the key the network was handed over to",
		"network", network, "from", from, "to", hubKey)
	return n, nil
}
