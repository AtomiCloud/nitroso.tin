package encryptor

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestDecryptRejectsMalformedValueWithoutPanicking(t *testing.T) {
	logger := zerolog.Nop()
	encr := NewSymEncryptor[string]("0123456789abcdef0123456789abcdef", &logger)
	if _, err := encr.Decrypt("not-encrypted"); err == nil {
		t.Fatal("expected malformed ciphertext error")
	}
}
