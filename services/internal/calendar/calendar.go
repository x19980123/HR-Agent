package calendar

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Slot struct {
	ID           string    `json:"id"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Location     string    `json:"location"`
	EventID      string    `json:"event_id,omitempty"`
	AttendeeIDs  []string  `json:"attendee_ids,omitempty"` // per-slot Feishu open_ids for Hold/addAttendees
}

// BookResult is returned when a calendar event is created (after candidate confirm).
type BookResult struct {
	EventID    string
	MeetingURL string
	Location   string
}

type Constraints struct {
	PreferredWindows []string  `json:"preferred_windows,omitempty"`
	After            time.Time `json:"after,omitempty"`
	Duration         time.Duration
	Limit            int
	// AttendeeIDs: when set, freebusy / Hold use this list instead of the global interviewer pool.
	AttendeeIDs []string `json:"attendee_ids,omitempty"`
}

// Provider abstracts Feishu/Google calendar.
type Provider interface {
	ListSlots(ctx context.Context, c Constraints) ([]Slot, error)
	Hold(ctx context.Context, slot Slot, applicationID string) (BookResult, error)
	Confirm(ctx context.Context, eventID string) error
	Release(ctx context.Context, eventID string) error
}

// BusyInterval is one busy block for a Feishu user (open_id).
type BusyInterval struct {
	OpenID    string    `json:"open_id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
}

// BusyLister is optional; used by Scheduling Agent to score panel overlap.
type BusyLister interface {
	ListBusy(ctx context.Context, from, to time.Time, openIDs []string) ([]BusyInterval, error)
}

// StaffSyncer is implemented by FeishuProvider for multi-interviewer + ACL sync.
type StaffSyncer interface {
	SetInterviewerUserIDs(ids []string)
	EnsureCalendarACL(ctx context.Context, userID string) error
	RemoveCalendarACL(ctx context.Context, userID string) error
}

// MemoryProvider is an MVP in-process calendar with weekday business hours.
type MemoryProvider struct {
	mu     sync.Mutex
	holds  map[string]Slot
	busy   map[string]bool
}

func NewMemoryProvider() *MemoryProvider {
	return &MemoryProvider{
		holds: map[string]Slot{},
		busy:  map[string]bool{},
	}
}

func (m *MemoryProvider) ListSlots(ctx context.Context, c Constraints) ([]Slot, error) {
	_ = ctx
	if c.Duration == 0 {
		c.Duration = time.Hour
	}
	if c.Limit <= 0 {
		c.Limit = 3
	}
	after := c.After
	if after.IsZero() {
		after = time.Now().Add(24 * time.Hour)
	}
	// snap to next weekday 10:00
	t := time.Date(after.Year(), after.Month(), after.Day(), 10, 0, 0, 0, after.Location())
	if t.Before(after) {
		t = t.Add(24 * time.Hour)
	}

	var out []Slot
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(out) < c.Limit {
		for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			t = t.Add(24 * time.Hour)
		}
		key := t.Format(time.RFC3339)
		if !m.busy[key] {
			out = append(out, Slot{
				ID:          uuid.NewString(),
				StartsAt:    t,
				EndsAt:      t.Add(c.Duration),
				Location:    "线上会议",
				AttendeeIDs: append([]string(nil), c.AttendeeIDs...),
			})
		}
		// next candidate: +2h same day or next day 10:00
		next := t.Add(2 * time.Hour)
		if next.Hour() >= 18 {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 10, 0, 0, 0, t.Location())
		} else {
			t = next
		}
	}
	return out, nil
}

func (m *MemoryProvider) Hold(ctx context.Context, slot Slot, applicationID string) (BookResult, error) {
	_ = ctx
	_ = applicationID
	m.mu.Lock()
	defer m.mu.Unlock()
	key := slot.StartsAt.Format(time.RFC3339)
	if m.busy[key] {
		return BookResult{}, fmt.Errorf("slot busy: %s", key)
	}
	eventID := uuid.NewString()
	slot.EventID = eventID
	m.holds[eventID] = slot
	m.busy[key] = true
	loc := slot.Location
	if loc == "" {
		loc = "线上会议"
	}
	return BookResult{
		EventID:    eventID,
		MeetingURL: "https://meeting.example.com/" + eventID[:8],
		Location:   loc,
	}, nil
}

func (m *MemoryProvider) Confirm(ctx context.Context, eventID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.holds[eventID]; !ok {
		return fmt.Errorf("unknown event %s", eventID)
	}
	return nil
}

func (m *MemoryProvider) Release(ctx context.Context, eventID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	slot, ok := m.holds[eventID]
	if !ok {
		return nil
	}
	delete(m.holds, eventID)
	delete(m.busy, slot.StartsAt.Format(time.RFC3339))
	return nil
}

// ListBusy returns no busy intervals (local/dev calendar).
func (m *MemoryProvider) ListBusy(ctx context.Context, from, to time.Time, openIDs []string) ([]BusyInterval, error) {
	_ = ctx
	_ = from
	_ = to
	_ = openIDs
	return nil, nil
}
