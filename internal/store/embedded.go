package store

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// EmbeddedPostgres manages the lifecycle of a local embedded PostgreSQL instance.
type EmbeddedPostgres struct {
	db   *embeddedpostgres.EmbeddedPostgres
	port uint32
}

// DefaultEmbeddedPort is the port used by the embedded Postgres instance.
const DefaultEmbeddedPort uint32 = 7434

// NewEmbeddedPostgres creates and starts an embedded Postgres instance.
// dataDir should be the persistent data directory (e.g. ~/.memoryd/data/pg).
func NewEmbeddedPostgres(dataDir string, port uint32) (*EmbeddedPostgres, error) {
	if port == 0 {
		port = DefaultEmbeddedPort
	}

	db := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Port(port).
			Database("memoryd").
			DataPath(filepath.Join(dataDir, "data")).
			RuntimePath(filepath.Join(dataDir, "runtime")).
			BinariesPath(filepath.Join(dataDir, "bin")).
			Logger(log.Writer()),
	)

	log.Printf("Starting embedded Postgres on port %d (data: %s)...", port, dataDir)
	if err := db.Start(); err != nil {
		return nil, fmt.Errorf("starting embedded postgres: %w", err)
	}
	log.Printf("Embedded Postgres running on port %d", port)

	return &EmbeddedPostgres{db: db, port: port}, nil
}

// ConnString returns the connection string for the embedded instance.
func (e *EmbeddedPostgres) ConnString() string {
	return fmt.Sprintf("postgres://postgres:postgres@localhost:%d/memoryd?sslmode=disable", e.port)
}

// Stop shuts down the embedded Postgres instance.
func (e *EmbeddedPostgres) Stop() error {
	if e.db != nil {
		log.Println("Stopping embedded Postgres...")
		return e.db.Stop()
	}
	return nil
}

// NewPostgresStoreLocal starts an embedded Postgres and returns a connected PostgresStore.
// The caller must call EmbeddedPostgres.Stop() on shutdown.
func NewPostgresStoreLocal(ctx context.Context, dataDir string) (*PostgresStore, *EmbeddedPostgres, error) {
	epg, err := NewEmbeddedPostgres(dataDir, DefaultEmbeddedPort)
	if err != nil {
		return nil, nil, err
	}

	store, err := NewPostgresStore(ctx, epg.ConnString())
	if err != nil {
		epg.Stop()
		return nil, nil, fmt.Errorf("connecting to embedded postgres: %w", err)
	}

	return store, epg, nil
}
