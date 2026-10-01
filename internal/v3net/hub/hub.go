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
	chatLimiter       *rateLimiter // per user: see allowChat
	chatNodeLimiter   *rateLimiter // per node, an outer cap over all its users
	nalStore          *NALStore
	nalMu             sync.Mutex // serializes NAL read-modify-write operations
	proposals         *ProposalStore
	accessRequests    *AccessRequestStore
	areaSubscriptions *AreaSubscriptionStore
	chatStore         *ChatHistoryStore
	chatRooms         *chatRooms

	// closeOnce makes Close idempotent; closeErr is the first call's result.
	closeOnce sync.Once
	closeErr  error
}

// New creates a new Hub with the given configuration.
func New(cfg Config) (*Hub, error) {
	dbPath := filepath.Join(cfg.DataDir, "hub.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("hub: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close() // cleanup on error path
		return nil, fmt.Errorf("hub: configure pragmas: %w", err)
	}

	subscribers, err := NewSubscriberStore(db)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	messages, err := NewMessageStore(db)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	nalStore, err := NewNALStore(db)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	proposals, err := NewProposalStore(db)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	accessReqs, err := NewAccessRequestStore(db)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	areaSubs, err := NewAreaSubscriptionStore(db)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	// Coordinator transfer was removed (#433): the hub signs the NAL, so the
	// coordinator is always the hub operator. Drop the pending-transfer table
	// older hubs created; best-effort, since a leftover table is inert.
	if _, err := db.Exec("DROP TABLE IF EXISTS coordinator_transfers"); err != nil {
		slog.Warn("hub: drop coordinator_transfers table", "error", err)
	}

	chatStore, err := NewChatHistoryStore(db, 7)
	if err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}

	h := &Hub{
		cfg:               cfg,
		db:                db,
		subscribers:       subscribers,
		messages:          messages,
		broadcaster:       NewBroadcaster(),
		chatLimiter:       newRateLimiter(chatUserInterval),
		chatNodeLimiter:   newBurstRateLimiter(chatNodeInterval, chatNodeBurst),
		nalStore:          nalStore,
		proposals:         proposals,
		accessRequests:    accessReqs,
		areaSubscriptions: areaSubs,
		chatStore:         chatStore,
		chatRooms:         newChatRooms(),
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
	h.chatStore.StartPruner(ctx)

	go func() {
		<-ctx.Done()
		_ = h.server.Close() // best-effort shutdown
	}()

	slog.Info("v3net hub starting", "addr", h.cfg.ListenAddr)

	err := h.server.ListenAndServe()

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Close gracefully shuts down the hub and releases resources. Shutdown paths
// often run more than once (a signal handler plus deferred cleanup), so only
// the first call does the work; later calls return its result.
func (h *Hub) Close() error {
	h.closeOnce.Do(func() { h.closeErr = h.close() })
	return h.closeErr
}

func (h *Hub) close() error {
	h.chatLimiter.Stop()
	h.chatNodeLimiter.Stop()
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
