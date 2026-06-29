// invitestore.go
package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// maxChannelsPerSender caps how many distinct channels we retain per inviter, a
// backstop against a single sender flooding the list. Surplus drops oldest-first.
const maxChannelsPerSender = 10

// pendingInvite is one received invite addressed to us, held in memory only.
type pendingInvite struct {
	Inviter    string
	Channel    string
	Trusted    bool
	ReceivedAt time.Time
	seq        int64 // sequence number for tie-breaking in sorts
}

// inviteStore is a session-only, in-memory store of received invites per network,
// with (sender, channel) dedup, a per-sender cap, a wall-clock TTL, and a
// per-network invite-block set. All times use the injected clock so tests can
// drive expiry deterministically.
type inviteStore struct {
	mu      sync.Mutex
	byNet   map[int64][]pendingInvite
	blocked map[int64]map[string]struct{}
	now     func() time.Time
	ttl     func() time.Duration
	seq     int64 // global sequence counter for tie-breaking
}

func newInviteStore(now func() time.Time, ttl func() time.Duration) *inviteStore {
	return &inviteStore{
		byNet:   map[int64][]pendingInvite{},
		blocked: map[int64]map[string]struct{}{},
		now:     now,
		ttl:     ttl,
	}
}

// add records an invite. It returns false when the sender is blocked (dropped).
// Repeats of the same (sender, channel) refresh the timestamp instead of stacking.
func (s *inviteStore) add(networkID int64, inviter, channel string, trusted bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if blk := s.blocked[networkID]; blk != nil {
		if _, b := blk[strings.ToLower(inviter)]; b {
			return false
		}
	}
	list := s.byNet[networkID]
	for i := range list {
		if strings.EqualFold(list[i].Inviter, inviter) && strings.EqualFold(list[i].Channel, channel) {
			list[i].ReceivedAt = s.now()
			list[i].Trusted = trusted
			s.seq++
			list[i].seq = s.seq
			s.byNet[networkID] = list
			return true
		}
	}
	s.seq++
	list = append(list, pendingInvite{Inviter: inviter, Channel: channel, Trusted: trusted, ReceivedAt: s.now(), seq: s.seq})
	list = capPerSender(list, inviter)
	s.byNet[networkID] = list
	return true
}

// capPerSender drops the oldest invite for inviter when they exceed the cap.
func capPerSender(list []pendingInvite, inviter string) []pendingInvite {
	idxs := make([]int, 0, len(list))
	for i := range list {
		if strings.EqualFold(list[i].Inviter, inviter) {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) <= maxChannelsPerSender {
		return list
	}
	oldest := idxs[0]
	for _, i := range idxs {
		if list[i].ReceivedAt.Before(list[oldest].ReceivedAt) {
			oldest = i
		}
	}
	out := make([]pendingInvite, 0, len(list)-1)
	out = append(out, list[:oldest]...)
	out = append(out, list[oldest+1:]...)
	return out
}

func (s *inviteStore) activeLocked(networkID int64) []pendingInvite {
	cutoff := s.now().Add(-s.ttl())
	out := make([]pendingInvite, 0, len(s.byNet[networkID]))
	for _, p := range s.byNet[networkID] {
		if p.ReceivedAt.After(cutoff) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReceivedAt == out[j].ReceivedAt {
			return out[i].seq > out[j].seq // newer sequence (higher number) comes first
		}
		return out[i].ReceivedAt.After(out[j].ReceivedAt)
	})
	return out
}

// list returns the non-expired invites for a network, newest-first.
func (s *inviteStore) list(networkID int64) []pendingInvite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeLocked(networkID)
}

// count returns the number of non-expired invites for a network.
func (s *inviteStore) count(networkID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.activeLocked(networkID))
}

func (s *inviteStore) dismissFrom(networkID int64, inviter string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeWhere(networkID, func(p pendingInvite) bool { return strings.EqualFold(p.Inviter, inviter) })
}

func (s *inviteStore) dismissOne(networkID int64, inviter, channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeWhere(networkID, func(p pendingInvite) bool {
		return strings.EqualFold(p.Inviter, inviter) && strings.EqualFold(p.Channel, channel)
	})
}

func (s *inviteStore) ignoreSender(networkID int64, inviter string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked[networkID] == nil {
		s.blocked[networkID] = map[string]struct{}{}
	}
	s.blocked[networkID][strings.ToLower(inviter)] = struct{}{}
	s.removeWhere(networkID, func(p pendingInvite) bool { return strings.EqualFold(p.Inviter, inviter) })
}

func (s *inviteStore) removeWhere(networkID int64, drop func(pendingInvite) bool) {
	src := s.byNet[networkID]
	kept := src[:0]
	for _, p := range src {
		if !drop(p) {
			kept = append(kept, p)
		}
	}
	s.byNet[networkID] = kept
}

// sweep drops expired invites everywhere and returns the network IDs that changed.
func (s *inviteStore) sweep() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-s.ttl())
	var changed []int64
	for net, src := range s.byNet {
		kept := src[:0]
		removed := false
		for _, p := range src {
			if p.ReceivedAt.After(cutoff) {
				kept = append(kept, p)
			} else {
				removed = true
			}
		}
		s.byNet[net] = kept
		if removed {
			changed = append(changed, net)
		}
	}
	return changed
}
