package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

const NotebookSessionCookieName = "addp_notebook_session"
const NotebookKernelCapabilityPrefix = "addp_nkc_"

var (
	ErrNotebookSessionNotFound = errors.New("notebook session not found")
	ErrNotebookSessionConflict = errors.New("notebook already has an active interactive session")
)

type NotebookSession struct {
	ID                   string    `json:"id"`
	TaskID               uint      `json:"task_id"`
	URL                  string    `json:"url"`
	ExpiresAt            time.Time `json:"expires_at"`
	TenantID             uint      `json:"-"`
	UserID               uint      `json:"-"`
	EngineID             uint      `json:"-"`
	Endpoint             string    `json:"-"`
	RuntimeToken         string    `json:"-"`
	ControlURL           string    `json:"-"`
	secretHash           [32]byte
	kernelCapabilityHash [32]byte
}

type NotebookSessionService struct {
	jupyter         *JupyterService
	tasks           *DevTaskService
	ttl             time.Duration
	ownerAPIBaseURL string
	mu              sync.RWMutex
	items           map[string]*NotebookSession
	stop            chan struct{}
	once            sync.Once
}

func NewNotebookSessionService(jupyter *JupyterService, tasks *DevTaskService, ttl time.Duration, ownerAPIBaseURL string) *NotebookSessionService {
	if ttl <= 0 {
		ttl = time.Hour
	}
	service := &NotebookSessionService{
		jupyter:         jupyter,
		tasks:           tasks,
		ttl:             ttl,
		ownerAPIBaseURL: strings.TrimRight(strings.TrimSpace(ownerAPIBaseURL), "/"),
		items:           make(map[string]*NotebookSession),
		stop:            make(chan struct{}),
	}
	go service.reap()
	return service
}

func (s *NotebookSessionService) Create(ctx context.Context, tenantID, userID, taskID uint) (*NotebookSession, string, error) {
	if s == nil || s.jupyter == nil || s.tasks == nil {
		return nil, "", fmt.Errorf("notebook session service is not configured")
	}
	task, err := s.tasks.GetDevTask(taskID, tenantID)
	if err != nil {
		return nil, "", ErrNotebookNotFound
	}
	if !task.IsNotebookScript() {
		return nil, "", ErrTaskNotNotebook
	}
	engineID := task.GetEngineID()
	if engineID == nil {
		return nil, "", fmt.Errorf("notebook task has no bound engine")
	}
	notebookPath, _ := task.Content["notebook_path"].(string)
	kernel, _ := task.Content["kernel"].(string)
	if strings.TrimSpace(kernel) == "" {
		kernel = "python3"
	}

	s.mu.Lock()
	for _, existing := range s.items {
		if existing.TenantID == tenantID && existing.TaskID == taskID && existing.ExpiresAt.After(time.Now()) {
			if existing.UserID != userID {
				s.mu.Unlock()
				return nil, "", ErrNotebookSessionConflict
			}
			secret, hash, secretErr := newNotebookSessionSecret()
			if secretErr != nil {
				s.mu.Unlock()
				return nil, "", secretErr
			}
			existing.secretHash = hash
			public := publicNotebookSession(existing)
			s.mu.Unlock()
			return public, secret, nil
		}
	}
	s.mu.Unlock()

	sessionID := uuid.NewString()
	basePath := "/api/v1/develop/notebook-sessions/" + sessionID + "/"
	secret, hash, err := newNotebookSessionSecret()
	if err != nil {
		return nil, "", err
	}
	if s.ownerAPIBaseURL == "" {
		return nil, "", fmt.Errorf("Develop service URL is required for notebook kernel capabilities")
	}
	kernelCapabilityToken, kernelCapabilityHash, err := newNotebookKernelCapability()
	if err != nil {
		return nil, "", err
	}
	ownerAPIEndpoint := s.ownerAPIBaseURL + "/api/v1/develop/notebook-kernel-sessions/" + url.PathEscape(sessionID) + "/engine-descriptors"
	runtimeSession, controlURL, err := s.jupyter.OpenInteractiveSession(ctx, tenantID, *engineID, plugin.InteractiveScriptSessionRequest{
		SessionID:            sessionID,
		TenantID:             tenantID,
		UserID:               userID,
		TaskID:               taskID,
		NotebookPath:         notebookPath,
		Kernel:               kernel,
		BasePath:             basePath,
		TTLSeconds:           int(s.ttl.Seconds()),
		OwnerAPIEndpoint:     ownerAPIEndpoint,
		OwnerCapabilityToken: kernelCapabilityToken,
	})
	if err != nil {
		return nil, "", err
	}
	session := &NotebookSession{
		ID:                   sessionID,
		TaskID:               taskID,
		URL:                  basePath + "lab/tree/" + url.PathEscape(runtimeSession.NotebookName),
		ExpiresAt:            runtimeSession.ExpiresAt,
		TenantID:             tenantID,
		UserID:               userID,
		EngineID:             *engineID,
		Endpoint:             runtimeSession.Endpoint,
		RuntimeToken:         runtimeSession.RuntimeToken,
		ControlURL:           controlURL,
		secretHash:           hash,
		kernelCapabilityHash: kernelCapabilityHash,
	}
	s.mu.Lock()
	s.items[sessionID] = session
	s.mu.Unlock()
	return publicNotebookSession(session), secret, nil
}

func (s *NotebookSessionService) ResolveKernelCapability(sessionID, token string) (*NotebookSession, error) {
	if !strings.HasPrefix(token, NotebookKernelCapabilityPrefix) {
		return nil, ErrNotebookSessionNotFound
	}
	s.mu.RLock()
	session := s.items[sessionID]
	if session == nil || !session.ExpiresAt.After(time.Now()) {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	provided := sha256.Sum256([]byte(token))
	valid := subtle.ConstantTimeCompare(session.kernelCapabilityHash[:], provided[:]) == 1
	if !valid {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	copy := *session
	s.mu.RUnlock()
	return &copy, nil
}

func (s *NotebookSessionService) ListQueryEngineDescriptors(ctx context.Context, sessionID, token string) ([]commonModels.EngineRuntimeDescriptor, error) {
	session, err := s.ResolveKernelCapability(sessionID, token)
	if err != nil {
		return nil, err
	}
	return s.jupyter.ListQueryEngines(ctx, session.TenantID)
}

func (s *NotebookSessionService) Resolve(sessionID, secret string) (*NotebookSession, error) {
	s.mu.RLock()
	session := s.items[sessionID]
	if session == nil || !session.ExpiresAt.After(time.Now()) {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	provided := sha256.Sum256([]byte(secret))
	valid := subtle.ConstantTimeCompare(session.secretHash[:], provided[:]) == 1
	if !valid {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	copy := *session
	s.mu.RUnlock()
	return &copy, nil
}

func (s *NotebookSessionService) Close(ctx context.Context, tenantID, userID, taskID uint, sessionID string) error {
	s.mu.Lock()
	session := s.items[sessionID]
	if session == nil || session.TenantID != tenantID || session.UserID != userID || session.TaskID != taskID {
		s.mu.Unlock()
		return ErrNotebookSessionNotFound
	}
	delete(s.items, sessionID)
	s.mu.Unlock()
	return s.jupyter.CloseInteractiveSession(ctx, session.TenantID, session.ControlURL, session.ID)
}

func (s *NotebookSessionService) Shutdown(ctx context.Context) {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	s.mu.Lock()
	items := make([]*NotebookSession, 0, len(s.items))
	for _, session := range s.items {
		items = append(items, session)
	}
	s.items = make(map[string]*NotebookSession)
	s.mu.Unlock()
	for _, session := range items {
		_ = s.jupyter.CloseInteractiveSession(ctx, session.TenantID, session.ControlURL, session.ID)
	}
}

func (s *NotebookSessionService) reap() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			expired := make([]*NotebookSession, 0)
			for id, session := range s.items {
				if !session.ExpiresAt.After(now) {
					expired = append(expired, session)
					delete(s.items, id)
				}
			}
			s.mu.Unlock()
			for _, session := range expired {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_ = s.jupyter.CloseInteractiveSession(ctx, session.TenantID, session.ControlURL, session.ID)
				cancel()
			}
		}
	}
}

func newNotebookSessionSecret() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate notebook session secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	return secret, sha256.Sum256([]byte(secret)), nil
}

func newNotebookKernelCapability() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate notebook kernel capability: %w", err)
	}
	token := NotebookKernelCapabilityPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func publicNotebookSession(session *NotebookSession) *NotebookSession {
	return &NotebookSession{ID: session.ID, TaskID: session.TaskID, URL: session.URL, ExpiresAt: session.ExpiresAt}
}
