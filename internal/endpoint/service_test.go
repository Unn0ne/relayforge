package endpoint

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/secure"
)

func TestCreateEncryptsGeneratedSecret(t *testing.T) {
	repository := &repositoryStub{}
	box, err := secure.NewBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repository, box, Options{})
	service.random = bytes.NewReader(bytes.Repeat([]byte{5}, 32))

	created, err := service.Create(context.Background(), CreateInput{
		Name:        " Billing ",
		URL:         "https://EXAMPLE.com/hooks?source=relayforge",
		Timeout:     2 * time.Second,
		MaxAttempts: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Endpoint.Name != "Billing" {
		t.Fatalf("name = %q", created.Endpoint.Name)
	}
	if created.Endpoint.URL != "https://example.com/hooks?source=relayforge" {
		t.Fatalf("URL = %q", created.Endpoint.URL)
	}
	if created.Secret == "" {
		t.Fatal("secret is empty")
	}
	plaintext, err := box.Decrypt(repository.created.SecretCiphertext, SecretContext(created.Endpoint.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != created.Secret {
		t.Fatalf("decrypted secret = %q", plaintext)
	}
}

func TestCreateAppliesDefaults(t *testing.T) {
	repository := &repositoryStub{}
	box, err := secure.NewBox(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repository, box, Options{})
	service.random = bytes.NewReader(make([]byte, 32))

	created, err := service.Create(context.Background(), CreateInput{Name: "Orders", URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Endpoint.Timeout != 5*time.Second || created.Endpoint.MaxAttempts != 8 {
		t.Fatalf("timeout=%s max_attempts=%d", created.Endpoint.Timeout, created.Endpoint.MaxAttempts)
	}
}

func TestCreateRejectsUnsafeTargets(t *testing.T) {
	service := New(&repositoryStub{}, cipherStub{}, Options{})
	tests := []string{
		"http://example.com/hooks",
		"https://user:password@example.com/hooks",
		"https://example.com/hooks#fragment",
		"https://localhost/hooks",
		"https://127.0.0.1/hooks",
		"https://10.0.0.1/hooks",
		"https://[::1]/hooks",
		"relative/path",
	}

	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			_, err := service.Create(context.Background(), CreateInput{Name: "target", URL: target})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCreateAllowsDevelopmentTargetWhenConfigured(t *testing.T) {
	repository := &repositoryStub{}
	service := New(repository, cipherStub{}, Options{AllowHTTP: true, AllowPrivateTargets: true})
	service.random = bytes.NewReader(make([]byte, 32))

	created, err := service.Create(context.Background(), CreateInput{Name: "local", URL: "http://127.0.0.1:9090/hook"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Endpoint.URL != "http://127.0.0.1:9090/hook" {
		t.Fatalf("URL = %q", created.Endpoint.URL)
	}
}

func TestGetRejectsMalformedID(t *testing.T) {
	service := New(&repositoryStub{}, cipherStub{}, Options{})
	_, err := service.Get(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v", err)
	}
}

type repositoryStub struct {
	created delivery.Endpoint
}

func (r *repositoryStub) CreateEndpoint(_ context.Context, value delivery.Endpoint) (delivery.Endpoint, error) {
	now := time.Now().UTC()
	value.CreatedAt = now
	value.UpdatedAt = now
	r.created = value
	return value, nil
}

func (r *repositoryStub) ListEndpoints(context.Context, int) ([]delivery.Endpoint, error) {
	return nil, nil
}

func (r *repositoryStub) GetEndpoint(context.Context, string) (delivery.Endpoint, error) {
	return delivery.Endpoint{}, nil
}

func (r *repositoryStub) DisableEndpoint(context.Context, string) (delivery.Endpoint, error) {
	return delivery.Endpoint{}, nil
}

type cipherStub struct{}

func (cipherStub) Encrypt(plaintext, _ []byte) ([]byte, error) {
	return append([]byte(nil), plaintext...), nil
}
