package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
)

// Pairing: the daemon prints an offer (QR + URI) carrying a single-use,
// short-lived 128-bit secret. A client posts the secret to /pair and gets a
// freshly minted device token scoped to the capabilities the admin chose.
// There is deliberately no bare PIN anywhere in the flow: a short code is
// brute-forceable at network speed, a 128-bit secret is not.

const (
	// DefaultPairTTL is how long an offer stays valid.
	DefaultPairTTL = 10 * time.Minute
	// maxPendingOffers bounds memory against runaway offer creation.
	maxPendingOffers = 16
)

var (
	ErrPairNoOffers  = errors.New("pairing is not enabled on this daemon (start it with -pair)")
	ErrPairBadSecret = errors.New("invalid or expired pairing secret")
	ErrPairTooMany   = errors.New("too many pending pairing offers; wait for one to expire")
	ErrPairName      = errors.New("device name is required")
)

// pairOffer is one outstanding invitation.
type pairOffer struct {
	secret   string
	name     string
	caps     []string
	uri      string
	expires  time.Time
	consumed bool
}

type pairManager struct {
	mu     sync.Mutex
	offers map[string]*pairOffer // keyed by secret
}

func newPairManager() *pairManager {
	return &pairManager{offers: make(map[string]*pairOffer)}
}

// pairRequest is the body the client POSTs to /pair.
type pairRequest struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
}

// pairResponse is the body /pair answers with.
type pairResponse struct {
	Token        string   `json:"token"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// CreatePairOffer mints an offer and returns the URI to encode/render.
// host is the address clients should connect to (scheme + host[:port]).
func (s *Server) CreatePairOffer(host, name string, caps []string, ttl time.Duration) (string, error) {
	if s.cfg.Devices == nil {
		return "", ErrPairNoOffers
	}
	if ttl <= 0 {
		ttl = DefaultPairTTL
	}
	secret, err := auth.Generate() // 256 bits; more than the 128-bit floor, same cost
	if err != nil {
		return "", fmt.Errorf("generate pairing secret: %w", err)
	}
	s.pair.mu.Lock()
	defer s.pair.mu.Unlock()
	// Drop expired or consumed offers first.
	now := time.Now()
	for k, o := range s.pair.offers {
		if o.consumed || now.After(o.expires) {
			delete(s.pair.offers, k)
		}
	}
	if len(s.pair.offers) >= maxPendingOffers {
		return "", ErrPairTooMany
	}
	offer := &pairOffer{secret: secret, name: name, caps: caps, expires: now.Add(ttl)}
	offer.uri = buildPairURI(host, secret, name)
	s.pair.offers[secret] = offer
	return offer.uri, nil
}

func buildPairURI(host, secret, name string) string {
	q := url.Values{}
	q.Set("host", host)
	q.Set("secret", secret)
	if name != "" {
		q.Set("name", name)
	}
	return "mexaroot://pair?" + q.Encode()
}

// consume validates and burns an offer. Constant-time compare so a wrong
// guess leaks nothing about how close it was.
func (s *Server) consumePairOffer(secret string) (*pairOffer, error) {
	s.pair.mu.Lock()
	defer s.pair.mu.Unlock()
	var match *pairOffer
	for k, o := range s.pair.offers {
		if !o.consumed && subtle.ConstantTimeCompare([]byte(k), []byte(secret)) == 1 {
			match = o
			break
		}
	}
	if match == nil || time.Now().After(match.expires) {
		return nil, ErrPairBadSecret
	}
	match.consumed = true
	return match, nil
}

// handlePair serves POST /pair. The response carries a minted device token;
// the offer burns regardless of whether the client keeps the answer.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req pairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "malformed body", http.StatusBadRequest)
		return
	}
	offer, err := s.consumePairOffer(req.Secret)
	if err != nil {
		if errors.Is(err, ErrPairBadSecret) {
			s.audit.write(AuditEntry{Event: "pair_failed", RemoteIP: hostOf(r.RemoteAddr)})
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.Name == "" {
		req.Name = offer.name
	}
	if !auth.ValidDeviceName(req.Name) {
		http.Error(w, auth.ErrDeviceName.Error(), http.StatusBadRequest)
		return
	}
	device, err := s.cfg.Devices.Create(req.Name, offer.caps)
	if err != nil {
		if errors.Is(err, auth.ErrDeviceExists) {
			// The offer is burned; the admin can mint another. Refuse
			// rather than silently re-pairing an existing name.
			http.Error(w, "device name already paired; revoke it first or choose another name", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit.write(AuditEntry{Event: "paired", RemoteIP: hostOf(r.RemoteAddr), Detail: device.Name})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pairResponse{
		Token:        device.Token,
		Name:         device.Name,
		Capabilities: device.Capabilities,
	})
}

// NormalizePairHost turns a listen address into the host form the URI
// advertises: loopback wildcard becomes 127.0.0.1.
func NormalizePairHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSuffix(addr, "/")
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
