package endpoint

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/google/uuid"
)

var (
	ErrInvalid  = errors.New("invalid endpoint")
	ErrNotFound = errors.New("endpoint not found")
)

type Repository interface {
	CreateEndpoint(context.Context, delivery.Endpoint) (delivery.Endpoint, error)
	ListEndpoints(context.Context, int) ([]delivery.Endpoint, error)
	GetEndpoint(context.Context, string) (delivery.Endpoint, error)
	DisableEndpoint(context.Context, string) (delivery.Endpoint, error)
}

type Cipher interface {
	Encrypt([]byte, []byte) ([]byte, error)
}

type Options struct {
	AllowHTTP           bool
	AllowPrivateTargets bool
}

type Service struct {
	repository Repository
	cipher     Cipher
	random     io.Reader
	options    Options
}

type CreateInput struct {
	Name        string
	URL         string
	Timeout     time.Duration
	MaxAttempts int
}

type Created struct {
	Endpoint delivery.Endpoint
	Secret   string
}

func New(repository Repository, cipher Cipher, options Options) *Service {
	return &Service{
		repository: repository,
		cipher:     cipher,
		random:     rand.Reader,
		options:    options,
	}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Created, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return Created{}, fmt.Errorf("%w: name must contain between 1 and 100 characters", ErrInvalid)
	}

	target, err := normalizeTarget(input.URL, s.options)
	if err != nil {
		return Created{}, err
	}

	timeout := input.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return Created{}, fmt.Errorf("%w: timeout must be between 100ms and 30s", ErrInvalid)
	}

	maxAttempts := input.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 8
	}
	if err = delivery.ValidateMaxAttempts(maxAttempts); err != nil {
		return Created{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	id := uuid.NewString()
	secretBytes := make([]byte, 32)
	if _, err = io.ReadFull(s.random, secretBytes); err != nil {
		return Created{}, fmt.Errorf("generate endpoint secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	ciphertext, err := s.cipher.Encrypt([]byte(secret), SecretContext(id))
	if err != nil {
		return Created{}, fmt.Errorf("encrypt endpoint secret: %w", err)
	}

	created, err := s.repository.CreateEndpoint(ctx, delivery.Endpoint{
		ID:               id,
		Name:             name,
		URL:              target,
		SecretCiphertext: ciphertext,
		Timeout:          timeout,
		MaxAttempts:      maxAttempts,
	})
	if err != nil {
		return Created{}, fmt.Errorf("create endpoint: %w", err)
	}
	return Created{Endpoint: created, Secret: secret}, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]delivery.Endpoint, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalid)
	}
	result, err := s.repository.ListEndpoints(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, id string) (delivery.Endpoint, error) {
	if _, err := uuid.Parse(id); err != nil {
		return delivery.Endpoint{}, fmt.Errorf("%w: malformed endpoint id", ErrInvalid)
	}
	result, err := s.repository.GetEndpoint(ctx, id)
	if err != nil {
		return delivery.Endpoint{}, fmt.Errorf("get endpoint: %w", err)
	}
	return result, nil
}

func (s *Service) Disable(ctx context.Context, id string) (delivery.Endpoint, error) {
	if _, err := uuid.Parse(id); err != nil {
		return delivery.Endpoint{}, fmt.Errorf("%w: malformed endpoint id", ErrInvalid)
	}
	result, err := s.repository.DisableEndpoint(ctx, id)
	if err != nil {
		return delivery.Endpoint{}, fmt.Errorf("disable endpoint: %w", err)
	}
	return result, nil
}

func SecretContext(id string) []byte {
	return []byte("relayforge:endpoint:" + id)
}

func normalizeTarget(raw string, options Options) (string, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: malformed URL", ErrInvalid)
	}

	target.Scheme = strings.ToLower(target.Scheme)
	if target.Scheme != "https" && (!options.AllowHTTP || target.Scheme != "http") {
		return "", fmt.Errorf("%w: URL must use HTTPS", ErrInvalid)
	}
	if target.Opaque != "" || target.Host == "" || target.User != nil || target.Fragment != "" {
		return "", fmt.Errorf("%w: URL must be absolute and cannot contain userinfo or fragment", ErrInvalid)
	}

	hostname := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", fmt.Errorf("%w: URL host is invalid", ErrInvalid)
	}
	if !options.AllowPrivateTargets && isPrivateHost(hostname) {
		return "", fmt.Errorf("%w: private targets are not allowed", ErrInvalid)
	}

	port := target.Port()
	if port != "" {
		target.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		target.Host = "[" + hostname + "]"
	} else {
		target.Host = hostname
	}
	if target.Path == "" {
		target.Path = "/"
	}
	return target.String(), nil
}

func isPrivateHost(hostname string) bool {
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		return false
	}
	return !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
