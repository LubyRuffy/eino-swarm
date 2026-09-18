package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Schedule kinds. Thread wakes land on an existing conversation; standalone
// jobs mint a new one per fire.
const (
	ScheduleThread     = "thread"
	ScheduleStandalone = "standalone"
)

// Schedule status values.
const (
	ScheduleActive    = "active"
	SchedulePaused    = "paused"
	ScheduleDone      = "done"
	ScheduleCancelled = "cancelled"
)

// Schedule run status values.
const (
	ScheduleRunSkippedBusy = "skipped_busy"
	ScheduleRunRunning     = "running"
	ScheduleRunFindings    = "findings"
	ScheduleRunQuiet       = "quiet"
	ScheduleRunError       = "error"
)

// Who created the schedule. Tools vs the human REST form.
const (
	ScheduleCreatedHuman   = "human"
	ScheduleCreatedManager = "manager"
)

// Schedule is a wall-clock wait: either a wake on an existing conversation
// or a standalone job that mints a conversation each time it fires.
type Schedule struct {
	ID             string `gorm:"primaryKey;size:64" json:"id"`
	Kind           string `gorm:"size:32" json:"kind"`
	OriginThreadID string `gorm:"index;size:64" json:"origin_thread_id"`
	// ThreadID is the conversation a wake lands on. Empty for standalone
	// rows: those do not reuse the origin conversation.
	ThreadID        string     `gorm:"index;size:64" json:"thread_id"`
	ProjectID       string     `gorm:"index;size:64" json:"project_id"`
	ProviderID      string     `gorm:"size:64" json:"provider_id"`
	Model           string     `gorm:"size:200" json:"model"`
	ReasoningEffort string     `gorm:"size:16" json:"reasoning_effort"`
	Title           string     `gorm:"size:400" json:"title"`
	Prompt          string     `json:"prompt"`
	DelayS          int        `json:"delay_s"`
	EveryS          int        `json:"every_s"`
	Cron            string     `gorm:"size:128" json:"cron"`
	Status          string     `gorm:"index:idx_sched_due;size:32" json:"status"`
	NextRunAt       time.Time  `gorm:"index:idx_sched_due" json:"next_run_at"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	RunCount        int        `json:"run_count"`
	MaxRuns         int        `json:"max_runs"`
	UntilAt         *time.Time `json:"until_at,omitempty"`
	CreatedBy       string     `gorm:"size:32" json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ScheduleRun is one fire (or a skipped tick) of a schedule.
type ScheduleRun struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	ScheduleID string     `gorm:"index;size:64" json:"schedule_id"`
	ThreadID   string     `gorm:"index;size:64" json:"thread_id"`
	TurnID     string     `gorm:"size:64" json:"turn_id"`
	Status     string     `gorm:"size:32" json:"status"`
	Summary    string     `json:"summary"`
	Unread     bool       `json:"unread"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// CreateSchedule inserts a schedule, filling in the id and timestamps.
func (s *Store) CreateSchedule(row *Schedule) error {
	if row.ID == "" {
		row.ID = NewID("sch_")
	}
	if row.Status == "" {
		row.Status = ScheduleActive
	}
	now := time.Now().UTC()
	row.CreatedAt, row.UpdatedAt = now, now
	if err := s.db.Create(row).Error; err != nil {
		return fmt.Errorf("store: create schedule: %w", err)
	}
	return nil
}

// GetSchedule loads one schedule.
func (s *Store) GetSchedule(id string) (*Schedule, error) {
	var row Schedule
	err := s.db.First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get schedule: %w", err)
	}
	return &row, nil
}

// ListDue returns active schedules whose next_run_at is at or before now.
// Paused rows stay out even if they are overdue, so a pause is not a
// catch-up queue.
func (s *Store) ListDue(now time.Time) ([]Schedule, error) {
	var out []Schedule
	err := s.db.Where("status = ? AND next_run_at <= ?", ScheduleActive, now.UTC()).
		Order("next_run_at asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list due schedules: %w", err)
	}
	return out, nil
}

// CountActive returns how many schedules are still armed. The ticker uses
// this against the configured cap; paused and cancelled rows must not count.
func (s *Store) CountActive() (int, error) {
	var n int64
	if err := s.db.Model(&Schedule{}).Where("status = ?", ScheduleActive).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("store: count active schedules: %w", err)
	}
	return int(n), nil
}

// CreateRun inserts a fire, filling in the id and timestamps.
func (s *Store) CreateRun(run *ScheduleRun) error {
	if run.ID == "" {
		run.ID = NewID("srun_")
	}
	if run.Status == "" {
		run.Status = ScheduleRunRunning
	}
	now := time.Now().UTC()
	run.CreatedAt, run.UpdatedAt = now, now
	if err := s.db.Create(run).Error; err != nil {
		return fmt.Errorf("store: create schedule run: %w", err)
	}
	return nil
}

// GetRun loads one schedule fire.
func (s *Store) GetRun(id string) (*ScheduleRun, error) {
	var run ScheduleRun
	err := s.db.First(&run, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get schedule run: %w", err)
	}
	return &run, nil
}

// FinishRun closes a fire with the status the inbox reads. Unread is passed
// as a map value so a quiet run can actually clear the flag — struct Updates
// would drop the false.
func (s *Store) FinishRun(id, status, summary string, unread bool) error {
	now := time.Now().UTC()
	res := s.db.Model(&ScheduleRun{}).Where("id = ?", id).Updates(map[string]any{
		"status":     status,
		"summary":    summary,
		"unread":     unread,
		"updated_at": now,
		"ended_at":   now,
	})
	if res.Error != nil {
		return fmt.Errorf("store: finish schedule run: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CancelSchedulesForThread marks wakes that target this conversation
// cancelled. Standalone jobs that only originated here keep running: they
// mint their own conversation per fire.
func (s *Store) CancelSchedulesForThread(threadID string) error {
	if err := cancelWakesForThread(s.db, threadID); err != nil {
		return fmt.Errorf("store: cancel schedules for thread: %w", err)
	}
	return nil
}

func cancelWakesForThread(db *gorm.DB, threadID string) error {
	if threadID == "" {
		return nil
	}
	now := time.Now().UTC()
	return db.Model(&Schedule{}).
		Where("thread_id = ? AND status IN ?", threadID, []string{ScheduleActive, SchedulePaused}).
		Updates(map[string]any{
			"status":     ScheduleCancelled,
			"updated_at": now,
		}).Error
}
