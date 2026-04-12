package store

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
// dataDir should be the persistent data directory (e.g. ~/.pgmemory/data/pg).
func NewEmbeddedPostgres(dataDir string, port uint32) (*EmbeddedPostgres, error) {
	if port == 0 {
		port = DefaultEmbeddedPort
	}

	db := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Port(port).
			Database("pgmemory").
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
	return fmt.Sprintf("postgres://postgres:postgres@localhost:%d/pgmemory?sslmode=disable", e.port)
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
// If an embedded PG is already running on the default port, it connects to it directly.
// The caller must call EmbeddedPostgres.Stop() on shutdown.
func NewPostgresStoreLocal(ctx context.Context, dataDir string) (*PostgresStore, *EmbeddedPostgres, error) {
	connStr := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/pgmemory?sslmode=disable", DefaultEmbeddedPort)
	binPath := filepath.Join(dataDir, "bin")

	// If something is already listening on the embedded port, try to connect directly.
	if portOpen(DefaultEmbeddedPort) {
		log.Printf("Embedded Postgres port %d already in use, attempting direct connect...", DefaultEmbeddedPort)

		if err := ensurePgvector(binPath); err != nil {
			log.Printf("pgvector setup warning: %v", err)
		}

		s, err := NewPostgresStore(ctx, connStr)
		if err == nil {
			log.Println("Connected to existing embedded Postgres")
			return s, &EmbeddedPostgres{port: DefaultEmbeddedPort}, nil
		}
		log.Printf("Could not connect to existing instance: %v — will try starting fresh", err)
	}

	epg, err := NewEmbeddedPostgres(dataDir, DefaultEmbeddedPort)
	if err != nil {
		return nil, nil, err
	}

	// Ensure pgvector extension files are present before migration creates the extension.
	if err := ensurePgvector(binPath); err != nil {
		epg.Stop()
		return nil, nil, fmt.Errorf("pgvector setup: %w", err)
	}

	store, err := NewPostgresStore(ctx, epg.ConnString())
	if err != nil {
		epg.Stop()
		return nil, nil, fmt.Errorf("connecting to embedded postgres: %w", err)
	}

	return store, epg, nil
}

// portOpen returns true if something is listening on the given TCP port.
func portOpen(port uint32) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*1e6) // 500ms
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensurePgvector checks if the pgvector extension is available in the embedded
// Postgres lib/share directories and copies it from system paths if missing.
func ensurePgvector(binariesPath string) error {
	libDir := filepath.Join(binariesPath, "lib", "postgresql")
	extDir := filepath.Join(binariesPath, "share", "postgresql", "extension")

	ext := "dylib"
	if runtime.GOOS == "linux" {
		ext = "so"
	}

	if _, err := os.Stat(filepath.Join(libDir, "vector."+ext)); err == nil {
		return nil
	}

	log.Println("pgvector extension not found in embedded Postgres, searching system...")

	pgMajor, err := detectPgMajorVersion(binariesPath)
	if err != nil {
		pgMajor = "18"
	}

	if runtime.GOOS == "darwin" {
		return installPgvectorFromBrew(pgMajor, libDir, extDir)
	}
	return installPgvectorFromSystem(pgMajor, libDir, extDir)
}

func detectPgMajorVersion(binariesPath string) (string, error) {
	out, err := exec.Command(filepath.Join(binariesPath, "bin", "postgres"), "--version").Output()
	if err != nil {
		return "", err
	}
	// "postgres (PostgreSQL) 18.3" → "18"
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) >= 3 {
		ver := parts[len(parts)-1]
		if idx := strings.IndexByte(ver, '.'); idx > 0 {
			return ver[:idx], nil
		}
	}
	return "", fmt.Errorf("could not parse version from: %s", string(out))
}

func installPgvectorFromBrew(pgMajor, libDir, extDir string) error {
	out, err := exec.Command("brew", "--prefix", "pgvector").Output()
	if err != nil {
		return fmt.Errorf("pgvector not installed; run: brew install pgvector")
	}
	prefix := strings.TrimSpace(string(out))
	pgName := "postgresql@" + pgMajor

	srcLib := filepath.Join(prefix, "lib", pgName, "vector.dylib")
	if _, err := os.Stat(srcLib); err != nil {
		return fmt.Errorf("vector.dylib not found at %s; try: brew reinstall pgvector", srcLib)
	}

	if err := copyFile(srcLib, filepath.Join(libDir, "vector.dylib")); err != nil {
		return fmt.Errorf("copying vector.dylib: %w", err)
	}

	srcExtDir := filepath.Join(prefix, "share", pgName, "extension")
	entries, err := os.ReadDir(srcExtDir)
	if err != nil {
		return fmt.Errorf("reading brew extension dir %s: %w", srcExtDir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "vector") {
			src := filepath.Join(srcExtDir, e.Name())
			dst := filepath.Join(extDir, e.Name())
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copying %s: %w", e.Name(), err)
			}
		}
	}

	log.Printf("pgvector extension installed from Homebrew (%s)", pgName)
	return nil
}

func installPgvectorFromSystem(pgMajor, libDir, extDir string) error {
	srcLib := filepath.Join("/usr/lib/postgresql", pgMajor, "lib", "vector.so")
	if _, err := os.Stat(srcLib); err != nil {
		return fmt.Errorf("pgvector not found; install with: sudo apt install postgresql-%s-pgvector", pgMajor)
	}

	if err := copyFile(srcLib, filepath.Join(libDir, "vector.so")); err != nil {
		return fmt.Errorf("copying vector.so: %w", err)
	}

	srcExtDir := filepath.Join("/usr/share/postgresql", pgMajor, "extension")
	entries, err := os.ReadDir(srcExtDir)
	if err != nil {
		return fmt.Errorf("reading system extension dir: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "vector") {
			src := filepath.Join(srcExtDir, e.Name())
			dst := filepath.Join(extDir, e.Name())
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copying %s: %w", e.Name(), err)
			}
		}
	}

	log.Printf("pgvector extension installed from system packages")
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	info, err := in.Stat()
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}
