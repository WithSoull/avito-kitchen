package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const databaseURLEnv = "POSTGRES_TEST_URL"

func CreateDatabase(t testing.TB, prefix string) string {
	t.Helper()
	adminURL := os.Getenv(databaseURLEnv)
	if adminURL == "" {
		t.Skipf("%s is not set", databaseURLEnv)
	}
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse %s: %v", databaseURLEnv, err)
	}

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate database suffix: %v", err)
	}
	name := strings.Trim(prefix, "_") + "_" + hex.EncodeToString(suffix[:])

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	if _, err := connection.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupConnection, err := pgx.Connect(cleanupCtx, adminURL)
		if err != nil {
			t.Errorf("connect for database cleanup: %v", err)
			return
		}
		defer func() { _ = cleanupConnection.Close(context.Background()) }()
		if _, err := cleanupConnection.Exec(cleanupCtx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	parsed.Path = "/" + name
	return parsed.String()
}

func ApplySQLFiles(t testing.TB, databaseURL string, relativePaths ...string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve project root")
	}
	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../.."))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	for _, relativePath := range relativePaths {
		contents, err := os.ReadFile(filepath.Join(projectRoot, relativePath))
		if err != nil {
			t.Fatalf("read SQL file %s: %v", relativePath, err)
		}
		if _, err := connection.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply SQL file %s: %v", relativePath, err)
		}
	}
}
