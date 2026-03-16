package hub

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Hub is the V3Net hub server.
type Hub struct {
	cfg         Config
	db          *sql.DB
	subscribers *SubscriberStore
	messages    *MessageStore
	broadcaster *Broadcaster
	server      *http.Server
	chatLimiter *rateLimiter
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

	h := &Hub{
		cfg:         cfg,
		db:          db,
		subscribers: subscribers,
		messages:    messages,
		broadcaster: NewBroadcaster(),
		chatLimiter: newRateLimiter(time.Second),
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.server.Shutdown(ctx)
	return h.db.Close()
}

// Subscribers returns the subscriber store (used in tests).
func (h *Hub) Subscribers() *SubscriberStore {
	return h.subscribers
}
