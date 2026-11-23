package testenv

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestComposeRunsTheTestImages(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join("..", "..", "..", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}

	for _, image := range []string{PostgresImage, RabbitMQImage} {
		// The newline makes the match exact: postgres:18.7-alpine must not match postgres:18.7-alpine3.24.
		if !bytes.Contains(compose, []byte("image: "+image+"\n")) {
			t.Errorf("compose.yaml does not run %s, which the tests use; change both together", image)
		}
	}
}
