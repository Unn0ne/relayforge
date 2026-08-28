package secure

import (
	"bytes"
	"errors"
	"testing"
)

func TestBoxRoundTrip(t *testing.T) {
	box, err := NewBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("webhook signing secret")
	additionalData := []byte("endpoint-id")
	first, err := box.Encrypt(plaintext, additionalData)
	if err != nil {
		t.Fatal(err)
	}
	second, err := box.Encrypt(plaintext, additionalData)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("ciphertexts must use different nonces")
	}

	decrypted, err := box.Decrypt(first, additionalData)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("plaintext = %q", decrypted)
	}
}

func TestBoxRejectsWrongContext(t *testing.T) {
	box, err := NewBox(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Encrypt([]byte("secret"), []byte("endpoint-a"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = box.Decrypt(ciphertext, []byte("endpoint-b"))
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("error = %v", err)
	}
}

func TestBoxRejectsTampering(t *testing.T) {
	box, err := NewBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Encrypt([]byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1

	_, err = box.Decrypt(ciphertext, nil)
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewBoxRejectsInvalidKey(t *testing.T) {
	if _, err := NewBox(make([]byte, 16)); err == nil {
		t.Fatal("expected key validation error")
	}
}

func TestBoxRejectsInvalidEnvelope(t *testing.T) {
	box, err := NewBox(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}

	tests := [][]byte{
		nil,
		{envelopeVersion},
		append([]byte{2}, make([]byte, 64)...),
	}
	for _, envelope := range tests {
		if _, err = box.Decrypt(envelope, nil); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("error = %v", err)
		}
	}
}
