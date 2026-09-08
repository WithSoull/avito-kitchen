package ordertoken_test

import (
	"testing"

	"github.com/WithSoull/avito-kitchen/internal/platform/security/ordertoken"
)

func TestGenerateAndHash(t *testing.T) {
	rawOne, hashOne, err := ordertoken.Generate()
	if err != nil {
		t.Fatal(err)
	}
	rawTwo, _, err := ordertoken.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if rawOne == rawTwo {
		t.Fatal("generated tokens must be unique")
	}
	got, err := ordertoken.Hash(rawOne)
	if err != nil {
		t.Fatal(err)
	}
	if got != hashOne {
		t.Fatalf("hash = %s, want %s", got, hashOne)
	}
	if _, err := ordertoken.Hash("invalid"); err == nil {
		t.Fatal("invalid token must fail")
	}
}
