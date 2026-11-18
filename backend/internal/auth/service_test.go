package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	users    map[string]User
	sessions map[string]Session
}

func newMemoryStore() *memoryStore {
	return &memoryStore{users: make(map[string]User), sessions: make(map[string]Session)}
}

func (s *memoryStore) FindUserByEmail(_ context.Context, email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (s *memoryStore) CreateSession(_ context.Context, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[string(session.TokenHash)] = session
	return nil
}

func (s *memoryStore) TouchSession(_ context.Context, hash []byte, now, idleExpiresAt time.Time) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[string(hash)]
	if !ok || !session.ExpiresAt.After(now) || !session.IdleExpiresAt.After(now) {
		return User{}, ErrNotFound
	}
	if idleExpiresAt.After(session.ExpiresAt) {
		idleExpiresAt = session.ExpiresAt
	}
	session.IdleExpiresAt = idleExpiresAt
	s.sessions[string(hash)] = session
	for _, user := range s.users {
		if user.ID == session.UserID && user.IsActive {
			return user, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *memoryStore) DeleteSession(_ context.Context, hash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, string(hash))
	return nil
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "a-long-test-password") {
		t.Fatalf("unexpected password hash: %q", hash)
	}
	if !VerifyPassword(hash, "a-long-test-password") {
		t.Fatal("correct password was rejected")
	}
	if VerifyPassword(hash, "wrong-password") || VerifyPassword("not-a-hash", "a-long-test-password") {
		t.Fatal("invalid password or hash was accepted")
	}
}

func TestLoginCreatesRandomHashedSessions(t *testing.T) {
	store := newMemoryStore()
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	store.users["admin@example.test"] = User{ID: 7, Email: "admin@example.test", PasswordHash: hash, DisplayName: "Admin", IsActive: true}
	service, err := NewService(store, 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.Login(context.Background(), " ADMIN@example.test ", "a-long-test-password", "127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(context.Background(), "admin@example.test", "a-long-test-password", "127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.User.ID != 7 || len(store.sessions) != 2 {
		t.Fatalf("unexpected sessions or user; session count = %d", len(store.sessions))
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.Token)
	if err != nil || len(decoded) != tokenBytes {
		t.Fatal("session token is not a random 32-byte value")
	}
	hashBytes := sha256.Sum256(decoded)
	session, ok := store.sessions[string(hashBytes[:])]
	if !ok || string(session.TokenHash) == first.Token || session.UserID != 7 {
		t.Fatal("session was not stored under the token hash")
	}
}

func TestLoginFailuresAreGeneric(t *testing.T) {
	store := newMemoryStore()
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	store.users["admin@example.test"] = User{ID: 1, Email: "admin@example.test", PasswordHash: hash, IsActive: true}
	service, err := NewService(store, 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, unknownErr := service.Login(context.Background(), "missing@example.test", "wrong-password", "127.0.0.1:1")
	_, wrongErr := service.Login(context.Background(), "admin@example.test", "wrong-password", "127.0.0.1:2")
	if !errors.Is(unknownErr, ErrInvalidCredentials) || !errors.Is(wrongErr, ErrInvalidCredentials) || unknownErr.Error() != wrongErr.Error() {
		t.Fatalf("errors differ: %v, %v", unknownErr, wrongErr)
	}
}

func TestLoginRateLimit(t *testing.T) {
	service, err := NewService(newMemoryStore(), 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLoginAttempts; i++ {
		if _, err := service.Login(context.Background(), "missing@example.test", "wrong", "127.0.0.1:1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v", i, err)
		}
	}
	if _, err := service.Login(context.Background(), "missing@example.test", "wrong", "127.0.0.1:1"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("rate-limited error = %v", err)
	}
}

func TestSessionIdleAndAbsoluteExpiration(t *testing.T) {
	store := newMemoryStore()
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	store.users["admin@example.test"] = User{ID: 1, Email: "admin@example.test", PasswordHash: hash, IsActive: true}
	service, err := NewService(store, 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	service.now = func() time.Time { return now }
	login, err := service.Login(context.Background(), "admin@example.test", "a-long-test-password", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := service.Authenticate(context.Background(), login.Token); err != nil {
		t.Fatalf("active session rejected: %v", err)
	}
	now = now.Add(14 * time.Minute)
	if _, err := service.Authenticate(context.Background(), login.Token); err != nil {
		t.Fatalf("sliding session rejected: %v", err)
	}
	now = now.Add(16 * time.Minute)
	if _, err := service.Authenticate(context.Background(), login.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("idle session error = %v", err)
	}

	now = time.Now()
	login, err = service.Login(context.Background(), "admin@example.test", "a-long-test-password", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Minute)
	if _, err := service.Authenticate(context.Background(), login.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("absolute expiration error = %v", err)
	}
}

func TestLogoutRevokesOnlyCurrentSession(t *testing.T) {
	store := newMemoryStore()
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	store.users["admin@example.test"] = User{ID: 1, Email: "admin@example.test", PasswordHash: hash, IsActive: true}
	service, err := NewService(store, 15*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := service.Login(context.Background(), "admin@example.test", "a-long-test-password", "127.0.0.1:1")
	second, _ := service.Login(context.Background(), "admin@example.test", "a-long-test-password", "127.0.0.1:1")
	if err := service.Logout(context.Background(), first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), first.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked session error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), second.Token); err != nil {
		t.Fatalf("other session was revoked: %v", err)
	}
}
