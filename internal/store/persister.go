package store

import (
	"context"
	"log/slog"

	"github.com/haepapa/kotui/pkg/models"
)

// StorePersister bridges the Dispatcher to the SQLite store.
// Register it as a subscriber on the Dispatcher; it will automatically
// persist every message it receives.
//
//	unsub := dispatcher.Subscribe("", persister.Handle)
//	defer unsub()
type StorePersister struct {
	db  *DB
	log *slog.Logger
}

// NewStorePersister creates a StorePersister backed by the given DB.
func NewStorePersister(db *DB, log *slog.Logger) *StorePersister {
	if log == nil {
		log = slog.Default()
	}
	return &StorePersister{db: db, log: log}
}

// Handle persists a message. It is safe to register as a Dispatcher subscriber.
// Errors are logged and silently dropped so a persistence failure never crashes
// the message bus.
func (p *StorePersister) Handle(msg models.Message) {
	if err := p.db.SaveMessage(context.Background(), msg); err != nil {
		p.log.Error("store persister: failed to save message",
			"msg_id", msg.ID,
			"kind", msg.Kind,
			"err", err,
		)
	}
}
