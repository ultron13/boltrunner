package postgres

import (
	"context"
	"os"
	"testing"
)

func TestConnectAndMigrate(t *testing.T) {
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()
	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestConnectMalformedDSN(t *testing.T) {
	ctx := context.Background()
	// pgxpool.New parses the DSN before ever dialing; an unparseable
	// connection string fails fast with no network access required.
	_, err := Connect(ctx, "not-a-valid-dsn://::::")
	if err == nil {
		t.Fatal("expected an error for a malformed DSN")
	}
}

func TestConnectPingFailure(t *testing.T) {
	ctx := context.Background()
	// A syntactically valid DSN pointing at a closed local port fails at the
	// Ping step rather than DSN parsing.
	_, err := Connect(ctx, "postgres://user:pass@127.0.0.1:1/nosuchdb?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected an error when the target refuses connections")
	}
}
