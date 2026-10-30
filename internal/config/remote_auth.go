package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RemoteAuthLegacy = "legacy"
const RemoteAuthNOSEC = "nosec"

var ErrRemoteApprovalRequired = errors.New("remote: NOSEC login and account approval are required")

// RemoteCredential lives only in the private host_token file. The atomic record
// binds a token to its origin and, after device authorization, its account.
type RemoteCredential struct {
	Schema     int    `json:"schema"`
	Token      string `json:"token"`
	Origin     string `json:"origin"`
	Authorized bool   `json:"authorized"`
	HostID     string `json:"host_id,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	Username   string `json:"username,omitempty"`
}

func RemoteOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("remote: a gateway origin without a path is required")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || net.ParseIP(host).IsLoopback())) {
		return "", errors.New("remote: gateway requires HTTPS")
	}
	port := u.Port()
	if port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		port = ""
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = ""
	u.RawPath = ""
	return u.String(), nil
}

func (c *Config) ReadRemoteCredential() (RemoteCredential, error) {
	raw, err := privateRemoteRead(c.remoteTokenPath())
	if os.IsNotExist(err) {
		return RemoteCredential{}, nil
	}
	if err != nil {
		return RemoteCredential{}, err
	}
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "{") {
		return RemoteCredential{Token: text}, nil
	}
	var credential RemoteCredential
	if json.Unmarshal(raw, &credential) != nil || credential.Schema != 1 || credential.Token == "" {
		return RemoteCredential{}, errors.New("remote: invalid private credential record")
	}
	return credential, nil
}

func (c *Config) AuthorizedHostToken() (string, error) {
	credential, err := c.ReadRemoteCredential()
	if err != nil {
		return "", err
	}
	origin, err := RemoteOrigin(c.Remote.HubURL)
	if err != nil {
		return "", err
	}
	if !credential.Authorized || credential.Origin != origin || credential.HostID == "" || credential.UserID == "" {
		return "", ErrRemoteApprovalRequired
	}
	return credential.Token, nil
}

// BindRemoteOrigin upgrades a legacy token only after a real socket connected.
// It records proof of its origin without granting NOSEC account authorization.
func (c *Config) BindRemoteOrigin(token, origin string) error {
	credential, err := c.ReadRemoteCredential()
	if err != nil {
		return err
	}
	if credential.Token != token {
		return errors.New("remote: credential changed during connection")
	}
	if credential.Origin != "" {
		if credential.Origin != origin {
			return errors.New("remote: credential belongs to another gateway")
		}
		return nil
	}
	credential.Schema = 1
	credential.Origin = origin
	return c.writeRemoteCredential(credential)
}

type PendingRemoteCredential struct {
	Token          string    `json:"token"`
	Origin         string    `json:"origin"`
	PublicKey      string    `json:"public_key"`
	CreatedAt      time.Time `json:"created_at"`
	ProvenRejected bool      `json:"proven_rejected,omitempty"`
}

const maxPendingRemoteCandidates = 128

type stagedRemoteCandidate struct {
	Path       string
	Credential PendingRemoteCredential
}

func (c *Config) pendingRemoteCandidates() ([]stagedRemoteCandidate, error) {
	entries, err := os.ReadDir(c.RemoteDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var candidates []stagedRemoteCandidate
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "pending-host-token-") {
			continue
		}
		if len(candidates) >= maxPendingRemoteCandidates {
			return nil, errors.New("remote: too many retained login candidates")
		}
		path := filepath.Join(c.RemoteDir(), entry.Name())
		raw, err := privateRemoteRead(path)
		if err != nil {
			return nil, err
		}
		var candidate PendingRemoteCredential
		if json.Unmarshal(raw, &candidate) != nil {
			return nil, errors.New("remote: invalid pending credential")
		}
		candidates = append(candidates, stagedRemoteCandidate{Path: path, Credential: candidate})
	}
	return candidates, nil
}
func (c *Config) PendingRemoteToken(origin, publicKey string) (PendingRemoteCredential, error) {
	candidates, err := c.pendingRemoteCandidates()
	if err != nil {
		return PendingRemoteCredential{}, err
	}
	var latest PendingRemoteCredential
	for _, entry := range candidates {
		candidate := entry.Credential
		if candidate.Origin == origin && candidate.PublicKey == publicKey && (latest.Token == "" || latest.ProvenRejected && !candidate.ProvenRejected || latest.ProvenRejected == candidate.ProvenRejected && candidate.CreatedAt.After(latest.CreatedAt)) {
			latest = candidate
		}
	}
	return latest, nil
}

func (c *Config) StageRemoteToken(token, origin, publicKey string) (string, error) {
	if err := c.privateRemoteDirectory(); err != nil {
		return "", err
	}
	candidates, err := c.pendingRemoteCandidates()
	if err != nil {
		return "", err
	}
	for _, entry := range candidates {
		prior := entry.Credential
		if prior.Token == token && prior.Origin == origin && prior.PublicKey == publicKey {
			return entry.Path, nil
		}
	}
	if len(candidates) >= maxPendingRemoteCandidates {
		return "", errors.New("remote: retained login candidate capacity reached")
	}
	file, err := os.CreateTemp(c.RemoteDir(), "pending-host-token-")
	if err != nil {
		return "", errors.New("remote: cannot stage connection credential")
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	raw, err := json.Marshal(PendingRemoteCredential{Token: token, Origin: origin, PublicKey: publicKey, CreatedAt: time.Now().UTC()})
	if err != nil {
		return "", err
	}
	if len(raw) > 16<<10 {
		return "", errors.New("remote: pending credential is too large")
	}
	if _, err = file.Write(raw); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = syncRemoteDirectory(c.RemoteDir()); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func (c *Config) RemovePendingRemoteToken(path string) error {
	if filepath.Dir(path) != c.RemoteDir() || !strings.HasPrefix(filepath.Base(path), "pending-host-token-") {
		return errors.New("remote: invalid pending credential path")
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *Config) ActivateRemoteToken(path string, credential RemoteCredential) error {
	if filepath.Dir(path) != c.RemoteDir() || !strings.HasPrefix(filepath.Base(path), "pending-host-token-") {
		return errors.New("remote: invalid pending credential path")
	}
	raw, err := privateRemoteRead(path)
	if err != nil {
		return err
	}
	var pending PendingRemoteCredential
	if json.Unmarshal(raw, &pending) != nil || pending.Token != credential.Token || pending.Origin != credential.Origin || !credential.Authorized || credential.Origin == "" || credential.HostID == "" || credential.UserID == "" {
		return errors.New("remote: incomplete authorized connection receipt")
	}
	credential.Schema = 1
	return c.writeRemoteCredential(credential)
}

func (c *Config) privateRemoteDirectory() error {
	if err := os.MkdirAll(c.RemoteDir(), dirPerm); err != nil {
		return err
	}
	info, err := os.Lstat(c.RemoteDir())
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("remote: unsafe credential directory")
	}
	return nil
}

func privateRemoteRead(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16<<10 {
		return nil, errors.New("remote: unsafe private credential file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, 16<<10))
}

func (c *Config) writeRemoteCredential(credential RemoteCredential) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return c.writeRemoteRecord(c.remoteTokenPath(), raw)
}
func (c *Config) writeRemoteRecord(path string, raw []byte) error {
	if err := c.privateRemoteDirectory(); err != nil {
		return err
	}
	file, err := os.CreateTemp(c.RemoteDir(), ".remote-record-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncRemoteDirectory(c.RemoteDir())
}

// SetPendingRemoteRejected records a definitive start rejection, or makes
// its proof uncertain again before a new request can reach the Gateway.
func (c *Config) SetPendingRemoteRejected(path string, rejected bool) error {
	if filepath.Dir(path) != c.RemoteDir() || !strings.HasPrefix(filepath.Base(path), "pending-host-token-") {
		return errors.New("remote: invalid pending credential path")
	}
	raw, err := privateRemoteRead(path)
	if err != nil {
		return err
	}
	var pending PendingRemoteCredential
	if json.Unmarshal(raw, &pending) != nil {
		return errors.New("remote: invalid pending credential")
	}
	pending.ProvenRejected = rejected
	raw, err = json.Marshal(pending)
	if err != nil {
		return err
	}
	return c.writeRemoteRecord(path, raw)
}
func (c *Config) RemoveRejectedRemoteScope(origin, publicKey string) error {
	candidates, err := c.pendingRemoteCandidates()
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		record := candidate.Credential
		if record.Origin == origin && record.PublicKey == publicKey && record.ProvenRejected {
			if err := c.RemovePendingRemoteToken(candidate.Path); err != nil {
				return err
			}
		}
	}
	return syncRemoteDirectory(c.RemoteDir())
}

func syncRemoteDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (c *Config) validateRemoteAuth() error {
	if c.Remote.AuthMode != "" && c.Remote.AuthMode != RemoteAuthLegacy && c.Remote.AuthMode != RemoteAuthNOSEC {
		return fmt.Errorf("remote: invalid authentication mode")
	}
	return nil
}

// ActivateRemoteTokenIfCurrent serializes the durable receipt with Settings
// updates. A late response for an origin or mode that already changed cannot
// replace the active private credential.
func (c *Config) ActivateRemoteTokenIfCurrent(path string, credential RemoteCredential) error {
	mu := c.mutex()
	mu.Lock()
	defer mu.Unlock()
	origin, err := RemoteOrigin(c.Remote.HubURL)
	if err != nil || origin != credential.Origin || !c.Remote.Enabled || c.Remote.AuthMode != RemoteAuthNOSEC {
		return errors.New("remote: login configuration changed")
	}
	return c.ActivateRemoteToken(path, credential)
}

// CompleteRemoteTokenActivation runs while the Host owns its login mutex.
// Retire older proofs durably before the current proof, so an interrupted
// cleanup retains a usable current candidate rather than an obsolete one.
func (c *Config) CompleteRemoteTokenActivation(path, origin, publicKey string) error {
	if filepath.Dir(path) != c.RemoteDir() || !strings.HasPrefix(filepath.Base(path), "pending-host-token-") {
		return errors.New("remote: invalid pending credential path")
	}
	candidates, err := c.pendingRemoteCandidates()
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if candidate.Path == path || candidate.Credential.Origin != origin || candidate.Credential.PublicKey != publicKey {
			continue
		}
		if err := c.RemovePendingRemoteToken(candidate.Path); err != nil {
			return err
		}
	}
	if err := syncRemoteDirectory(c.RemoteDir()); err != nil {
		return err
	}
	if err := c.RemovePendingRemoteToken(path); err != nil {
		return err
	}
	return syncRemoteDirectory(c.RemoteDir())
}
