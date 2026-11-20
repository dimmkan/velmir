package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	tokenBytes         = 32
	maxLoginAttempts   = 5
	maxIPLoginAttempts = 20
	loginAttemptWindow = 15 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTooManyAttempts    = errors.New("too many login attempts")
	ErrUnauthorized       = errors.New("unauthorized")
)

type LoginResult struct {
	Token string
	User  User
}

type attempt struct {
	count       int
	windowStart time.Time
}

type Service struct {
	store           Store
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
	dummyHash       string
	now             func() time.Time
	mu              sync.Mutex
	attempts        map[string]attempt
}

func NewService(store Store, idleTimeout, absoluteTimeout time.Duration) (*Service, error) {
	if idleTimeout <= 0 || absoluteTimeout <= 0 || absoluteTimeout < idleTimeout {
		return nil, errors.New("session timeouts must be positive and absolute timeout must be at least idle timeout")
	}
	randomPassword := make([]byte, tokenBytes)
	if _, err := rand.Read(randomPassword); err != nil {
		return nil, fmt.Errorf("initialize password verifier: %w", err)
	}
	dummyHash, err := HashPassword(base64.RawURLEncoding.EncodeToString(randomPassword))
	if err != nil {
		return nil, err
	}
	return &Service{
		store: store, idleTimeout: idleTimeout, absoluteTimeout: absoluteTimeout,
		dummyHash: dummyHash, now: time.Now, attempts: make(map[string]attempt),
	}, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) Login(ctx context.Context, email, password, remoteAddr string) (LoginResult, error) {
	email = NormalizeEmail(email)
	ip := remoteIP(remoteAddr)
	if s.limited(email, ip) {
		return LoginResult{}, ErrTooManyAttempts
	}

	user, err := s.store.FindUserByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return LoginResult{}, err
	}
	passwordHash := s.dummyHash
	if err == nil {
		passwordHash = user.PasswordHash
	}
	valid := VerifyPassword(passwordHash, password)
	if err != nil || !valid || !user.IsActive {
		s.recordFailure(email, ip)
		return LoginResult{}, ErrInvalidCredentials
	}

	token, tokenHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now()
	expiresAt := now.Add(s.absoluteTimeout)
	idleExpiresAt := now.Add(s.idleTimeout)
	if idleExpiresAt.After(expiresAt) {
		idleExpiresAt = expiresAt
	}
	if err := s.store.CreateSession(ctx, Session{
		UserID: user.ID, TokenHash: tokenHash[:], CreatedAt: now, ExpiresAt: expiresAt, IdleExpiresAt: idleExpiresAt,
	}); err != nil {
		return LoginResult{}, err
	}
	s.resetFailures(email, ip)
	return LoginResult{Token: token, User: user}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	tokenHash, err := hashToken(token)
	if err != nil {
		return User{}, ErrUnauthorized
	}
	now := s.now()
	user, err := s.store.TouchSession(ctx, tokenHash[:], now, now.Add(s.idleTimeout))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrUnauthorized
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	tokenHash, err := hashToken(token)
	if err != nil {
		return ErrUnauthorized
	}
	return s.store.DeleteSession(ctx, tokenHash[:])
}

func newToken() (string, [sha256.Size]byte, error) {
	bytes := make([]byte, tokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	hash, _ := hashToken(token)
	return token, hash, nil
}

func hashToken(token string) ([sha256.Size]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != tokenBytes {
		return [sha256.Size]byte{}, ErrUnauthorized
	}
	return sha256.Sum256(decoded), nil
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func (s *Service) limited(email, ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	return s.attemptCountLocked("email:"+ip+":"+email, now) >= maxLoginAttempts ||
		s.attemptCountLocked("ip:"+ip, now) >= maxIPLoginAttempts
}

func (s *Service) attemptCountLocked(key string, now time.Time) int {
	entry := s.attempts[key]
	if now.Sub(entry.windowStart) >= loginAttemptWindow {
		delete(s.attempts, key)
		return 0
	}
	return entry.count
}

func (s *Service) recordFailure(email, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.incrementFailureLocked("email:"+ip+":"+email, now)
	s.incrementFailureLocked("ip:"+ip, now)
	if len(s.attempts) > 10000 {
		for key, entry := range s.attempts {
			if now.Sub(entry.windowStart) >= loginAttemptWindow {
				delete(s.attempts, key)
			}
		}
	}
}

func (s *Service) incrementFailureLocked(key string, now time.Time) {
	entry := s.attempts[key]
	if now.Sub(entry.windowStart) >= loginAttemptWindow {
		entry = attempt{windowStart: now}
	}
	entry.count++
	s.attempts[key] = entry
}

func (s *Service) resetFailures(email, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, "email:"+ip+":"+email)
	delete(s.attempts, "ip:"+ip)
}

func (s *Service) CleanupLoginAttempts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, entry := range s.attempts {
		if now.Sub(entry.windowStart) >= loginAttemptWindow {
			delete(s.attempts, key)
		}
	}
}
