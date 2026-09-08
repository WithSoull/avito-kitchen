package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/repository"
)

func TestCurrentMenuReflectsSeedAndStock(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "venue_menu")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/venue/000001_init.up.sql",
		"migrations/venue/000002_menu_version_as_bigint.up.sql",
		"migrations/venue/000003_query_indexes.up.sql",
		"migrations/venue/000004_seed_menu.up.sql",
	)
	database, err := postgres.Open(context.Background(), postgres.Config{
		URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: time.Second,
		MaxConns: 4, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := repository.New(database, database)

	snapshot, err := repo.CurrentMenu(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != 1 || len(snapshot.Categories) != 2 || countItems(snapshot) != 3 {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	if _, err := database.Exec(context.Background(), "UPDATE venue_menu_items SET stock_quantity=0 WHERE external_id='cola'"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repo.CurrentMenu(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range snapshot.Categories {
		for _, item := range category.Items {
			if item.ExternalID == "cola" && item.IsAvailable {
				t.Fatal("zero-stock item must be published as unavailable")
			}
		}
	}
}

func countItems(snapshot domain.MenuSnapshot) int {
	total := 0
	for _, category := range snapshot.Categories {
		total += len(category.Items)
	}
	return total
}
