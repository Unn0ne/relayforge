package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const envelopeVersion byte = 1

var ErrInvalidCiphertext = errors.New("invalid ciphertext")

type Box struct {
	aead cipher.AEAD
}

func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plaintext, additionalData []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	envelope := make([]byte, 1+len(nonce))
	envelope[0] = envelopeVersion
	copy(envelope[1:], nonce)
	return b.aead.Seal(envelope, nonce, plaintext, additionalData), nil
}

func (b *Box) Decrypt(envelope, additionalData []byte) ([]byte, error) {
	nonceSize := b.aead.NonceSize()
	if len(envelope) < 1+nonceSize+b.aead.Overhead() || envelope[0] != envelopeVersion {
		return nil, ErrInvalidCiphertext
	}

	nonce := envelope[1 : 1+nonceSize]
	ciphertext := envelope[1+nonceSize:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	return plaintext, nil
}
