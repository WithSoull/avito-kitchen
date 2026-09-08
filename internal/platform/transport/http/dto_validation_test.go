package httptransport

import (
	"strings"
	"testing"
)

func TestOpenAPIStringConstraints(t *testing.T) {
	if !validRequiredText(strings.Repeat("я", 200), 200) {
		t.Fatal("200 Unicode characters must be accepted")
	}
	if validRequiredText(strings.Repeat("я", 201), 200) {
		t.Fatal("201 Unicode characters must be rejected")
	}
	if !validCurrency("RUB") || validCurrency("rub") || validCurrency("₽") {
		t.Fatal("currency must match ^[A-Z]{3}$")
	}
}
