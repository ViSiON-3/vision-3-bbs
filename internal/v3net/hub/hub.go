package hub

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Hub is the V3Net hub server.
type Hub struct {
	cfg               Config
	db                *sql.DB
	subscribers       *SubscriberStore
	messages          *MessageStore
	broadcaster       *Broadcaster
	server            *http.Server
	chatLimiter       *rateLimiter
	nalStore          *NALStore
	nalMu             sync.Mutex // serializes NAL read-modify-write operations
	proposals         *ProposalStore
	accessRequests    *AccessRequestStore
	areaSubscriptions *AreaSubscriptionStore
	coordTransfers    *CoordTransferStore
}

// New creates a new Hub with the given configuration.
func New(cfg Config) (*Hub, error) {
	dbPath := filepath.Join(cfg.DataDir, "hub.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("hub: open database: %w", err)
	}

	subscribers, err := NewSubscriberStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	messages, err := NewMessageStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	nalStore, err := NewNALStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	proposals, err := NewProposalStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	accessReqs, err := NewAccessRequestStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	areaSubs, err := NewAreaSubscriptionStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	coordTransfers, err := NewCoordTransferStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	h := &Hub{
		cfg:               cfg,
		db:                db,
		subscribers:       subscribers,
		messages:          messages,
		broadcaster:       NewBroadcaster(),
		chatLimiter:       newRateLimiter(time.Second),
		nalStore:          nalStore,
		proposals:         proposals,
		accessRequests:    accessReqs,
		areaSubscriptions: areaSubs,
		coordTransfers:    coordTransfers,
	}

	h.server = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: h.newMux(),
	}

	return h, nil
}

// Start begins serving HTTP and the ping broadcaster. It blocks until the
// context is cancelled or the server encounters a fatal error.
func (h *Hub) Start(ctx context.Context) error {
	go h.broadcaster.StartPing(ctx)

	go func() {
		<-ctx.Done()
		h.server.Close()
	}()

	slog.Info("v3net hub starting", "addr", h.cfg.ListenAddr)

	var err error
	if h.cfg.TLSCertFile != "" && h.cfg.TLSKeyFile != "" {
		err = h.server.ListenAndServeTLS(h.cfg.TLSCertFile, h.cfg.TLSKeyFile)
	} else {
		err = h.server.ListenAndServe()
	}

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Close gracefully shuts down the hub and releases resources.
func (h *Hub) Close() error {
	h.chatLimiter.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := h.server.Shutdown(ctx)
	dbErr := h.db.Close()
	if shutdownErr != nil && dbErr != nil {
		return fmt.Errorf("hub: shutdown: %w; db close: %v", shutdownErr, dbErr)
	}
	if shutdownErr != nil {
		return shutdownErr
	}
	return dbErr
}

// Subscribers returns the subscriber store (used in tests).
func (h *Hub) Subscribers() *SubscriberStore {
	return h.subscribers
}

// NALStore returns the NAL store (used for in-process seeding at startup).
func (h *Hub) NALStore() *NALStore {
	return h.nalStore
}
