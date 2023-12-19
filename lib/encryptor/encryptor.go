package encryptor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"strings"
)

type Encryptor[T any] interface {
	Encrypt(s string) (string, error)
	Decrypt(s string) (string, error)
	EncryptAny(obj T) (string, error)
	DecryptAny(s string) (T, error)
}

type SymEncryptor[T any] struct {
	key    string
	logger *zerolog.Logger
}

func NewSymEncryptor[T any](key string, logger *zerolog.Logger) Encryptor[T] {
	e := &SymEncryptor[T]{
		key:    key,
		logger: logger,
	}
	return e
}

func (e *SymEncryptor[T]) Encrypt(s string) (string, error) {
	block, err := aes.NewCipher([]byte(e.key))
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to create cipher")
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to create gcm")
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, er := rand.Read(nonce); er != nil {
		e.logger.Error().Err(er).Msg("Failed to create nonce")
		return "", er
	}

	plaintext := []byte(s)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	ciphertextHex := hex.EncodeToString(ciphertext)
	nonceString := hex.EncodeToString(nonce)
	return fmt.Sprintf("%s:%s", nonceString, ciphertextHex), nil

}

func (e *SymEncryptor[T]) Decrypt(s string) (string, error) {
	block, err := aes.NewCipher([]byte(e.key))
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to create cipher")
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to create gcm")
		return "", err
	}

	split := strings.Split(s, ":")
	nonceHex := split[0]
	ciphertextHex := split[1]
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to decode nonce")
		return "", err
	}
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to decode ciphertext")
		return "", err
	}

	decrypted, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to decrypt")
		return "", err
	}
	return string(decrypted), nil
}

func (e *SymEncryptor[T]) EncryptAny(obj T) (string, error) {

	marshal, err := json.Marshal(obj)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to marshal")
		return "", err
	}
	cipher, err := e.Encrypt(string(marshal))
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to encrypt")
		return "", err
	}
	return cipher, nil
}

func (e *SymEncryptor[T]) DecryptAny(s string) (T, error) {
	var t T
	decrypted, err := e.Decrypt(s)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to decrypt")
		return t, err
	}
	err = json.Unmarshal([]byte(decrypted), &t)
	if err != nil {
		e.logger.Error().Err(err).Msg("Failed to unmarshal")
		return t, err
	}
	return t, nil
}
