package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
)

func TestPoolTransactionsAndStatementTimeout(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "postgres_foundation")
	database, err := postgres.Open(context.Background(), postgres.Config{
		URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: 100 * time.Millisecond,
		MaxConns: 2, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(context.Background(), "CREATE TABLE records (id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	readiness := postgres.NewMigrationReadiness(database, 4)
	if err := readiness.Ping(context.Background()); err == nil {
		t.Fatal("empty schema must not be ready")
	}
	if _, err := database.Exec(context.Background(), "CREATE TABLE schema_migrations (version bigint PRIMARY KEY, dirty boolean NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(context.Background(), "INSERT INTO schema_migrations (version, dirty) VALUES (4, false)"); err != nil {
		t.Fatal(err)
	}
	if err := readiness.Ping(context.Background()); err != nil {
		t.Fatalf("migrated schema is not ready: %v", err)
	}
	wantRollback := errors.New("rollback")
	err = database.WithinTransaction(context.Background(), func(ctx context.Context, tx postgres.DBTX) error {
		if _, err := tx.Exec(ctx, "INSERT INTO records (id) VALUES (1)"); err != nil {
			return err
		}
		return wantRollback
	})
	if !errors.Is(err, wantRollback) {
		t.Fatalf("transaction error = %v", err)
	}

	var count int
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM records").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled back row count = %d", count)
	}
	if err := database.WithinTransaction(context.Background(), func(ctx context.Context, tx postgres.DBTX) error {
		_, err := tx.Exec(ctx, "INSERT INTO records (id) VALUES (2)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM records").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed row count = %d", count)
	}

	if err := database.QueryRow(context.Background(), "SELECT pg_sleep(0.5)").Scan(new(any)); err == nil {
		t.Fatal("expected statement timeout")
	}
}
