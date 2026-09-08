package service

import (
	"strings"
	"testing"
)

func TestRequiredTextUsesCharacterLimit(t *testing.T) {
	if !validRequiredText(strings.Repeat("я", 200), 200) {
		t.Fatal("200 Unicode characters must be accepted")
	}
	if validRequiredText(strings.Repeat("я", 201), 200) {
		t.Fatal("201 Unicode characters must be rejected")
	}
}
