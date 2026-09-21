package store

import (
	"errors"
	"fmt"
	"strings"
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

// ErrScheduleCap means an insert, resume, or done→active rearm would
// exceed the active ceiling. Count and write share one transaction; a
// check-then-write pair is a race.
var ErrScheduleCap = errors.New("store: too many active schedules")

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

func prepareSchedule(row *Schedule) {
	if row.ID == "" {
		row.ID = NewID("sch_")
	}
	if row.Status == "" {
		row.Status = ScheduleActive
	}
	now := time.Now().UTC()
	row.CreatedAt, row.UpdatedAt = now, now
	row.NextRunAt = row.NextRunAt.UTC()
	if row.UntilAt != nil {
		u := row.UntilAt.UTC()
		row.UntilAt = &u
	}
	if row.LastRunAt != nil {
		u := row.LastRunAt.UTC()
		row.LastRunAt = &u
	}
}

func insertSchedule(db *gorm.DB, row *Schedule) error {
	prepareSchedule(row)
	if err := db.Create(row).Error; err != nil {
		return fmt.Errorf("store: create schedule: %w", err)
	}
	return nil
}

// CreateSchedule inserts a schedule, filling in the id and timestamps.
func (s *Store) CreateSchedule(row *Schedule) error {
	return insertSchedule(s.db, row)
}

// CreateScheduleUnderCap inserts an active row only if CountActive is
// still below cap. The count and the insert share one transaction.
func (s *Store) CreateScheduleUnderCap(row *Schedule, cap int) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		n, err := countActive(tx)
		if err != nil {
			return err
		}
		if n >= cap {
			return ErrScheduleCap
		}
		return insertSchedule(tx, row)
	})
}

func countActive(db *gorm.DB) (int, error) {
	var n int64
	if err := db.Model(&Schedule{}).Where("status = ?", ScheduleActive).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("store: count active schedules: %w", err)
	}
	return int(n), nil
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
	return countActive(s.db)
}

// ListRuns returns every fire of one schedule, oldest first.
func (s *Store) ListRuns(scheduleID string) ([]ScheduleRun, error) {
	var out []ScheduleRun
	err := s.db.Where("schedule_id = ?", scheduleID).Order("created_at asc, id asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list schedule runs: %w", err)
	}
	return out, nil
}

// CountRunningRuns is how many fires this process has claimed and not yet
// finished. The ticker uses it as the concurrent-fire cap.
func (s *Store) CountRunningRuns() (int, error) {
	var n int64
	if err := s.db.Model(&ScheduleRun{}).Where("status = ?", ScheduleRunRunning).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("store: count running schedule runs: %w", err)
	}
	return int(n), nil
}

// HasPendingThreadWake is true when this conversation still has a thread
// wake that should own the next turn: an armed row, or a claimed fire
// whose run is still running. Claim marks a delay one-shot done before
// StartTurn; listing only status=active would let /goal steal that slot.
func (s *Store) HasPendingThreadWake(threadID string) (bool, error) {
	if threadID == "" {
		return false, nil
	}
	var n int64
	err := s.db.Model(&Schedule{}).
		Where("kind = ? AND thread_id = ? AND status = ?", ScheduleThread, threadID, ScheduleActive).
		Limit(1).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("store: has pending thread wake: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	err = s.db.Model(&ScheduleRun{}).
		Joins("JOIN schedules ON schedules.id = schedule_runs.schedule_id").
		Where("schedule_runs.status = ? AND schedules.kind = ? AND (schedules.thread_id = ? OR schedule_runs.thread_id = ?)",
			ScheduleRunRunning, ScheduleThread, threadID, threadID).
		Limit(1).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("store: has pending thread wake: %w", err)
	}
	return n > 0, nil
}

// ActiveThreadWake is the soonest armed thread wait on this conversation.
// Nil when nothing is parked. The phone banner and Run now / Cancel wait
// target this row; dumping every schedule onto pairlink would be the inbox.
func (s *Store) ActiveThreadWake(threadID string) (*Schedule, error) {
	if threadID == "" {
		return nil, nil
	}
	var row Schedule
	err := s.db.Where("kind = ? AND thread_id = ? AND status = ?", ScheduleThread, threadID, ScheduleActive).
		Order("next_run_at ASC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: active thread wake: %w", err)
	}
	return &row, nil
}

// PendingWakeThreadIDs is every conversation whose next turn is a parked
// wait: an armed thread wake, or a claimed thread fire still running.
// The sidebar listing uses this in one pass so it does not query per row.
func (s *Store) PendingWakeThreadIDs() ([]string, error) {
	var armed []string
	err := s.db.Model(&Schedule{}).
		Where("kind = ? AND status = ? AND thread_id <> ?", ScheduleThread, ScheduleActive, "").
		Distinct("thread_id").Pluck("thread_id", &armed).Error
	if err != nil {
		return nil, fmt.Errorf("store: pending wake thread ids: %w", err)
	}
	var claimed []string
	err = s.db.Model(&ScheduleRun{}).
		Joins("JOIN schedules ON schedules.id = schedule_runs.schedule_id").
		Where("schedule_runs.status = ? AND schedules.kind = ?", ScheduleRunRunning, ScheduleThread).
		Pluck("schedule_runs.thread_id", &claimed).Error
	if err != nil {
		return nil, fmt.Errorf("store: pending wake run thread ids: %w", err)
	}
	var fromSched []string
	err = s.db.Model(&ScheduleRun{}).
		Joins("JOIN schedules ON schedules.id = schedule_runs.schedule_id").
		Where("schedule_runs.status = ? AND schedules.kind = ? AND schedules.thread_id <> ?",
			ScheduleRunRunning, ScheduleThread, "").
		Pluck("schedules.thread_id", &fromSched).Error
	if err != nil {
		return nil, fmt.Errorf("store: pending wake schedule thread ids: %w", err)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(armed)+len(claimed)+len(fromSched))
	for _, id := range append(append(armed, claimed...), fromSched...) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// HasRunningRun is true when this wait already has a claimed fire. Two ticks
// must not start two turns for one row.
func (s *Store) HasRunningRun(scheduleID string) (bool, error) {
	var n int64
	err := s.db.Model(&ScheduleRun{}).
		Where("schedule_id = ? AND status = ?", scheduleID, ScheduleRunRunning).
		Limit(1).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("store: has running schedule run: %w", err)
	}
	return n > 0, nil
}

// SetRunTurn binds the turn the fire started. Claim happens before
// CreateTurn, so the id lands afterwards.
func (s *Store) SetRunTurn(id, turnID string) error {
	now := time.Now().UTC()
	res := s.db.Model(&ScheduleRun{}).Where("id = ?", id).Updates(map[string]any{
		"turn_id":    turnID,
		"updated_at": now,
	})
	if res.Error != nil {
		return fmt.Errorf("store: bind schedule run turn: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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

// MarkRunRead clears unread on one fire. Map Updates so false is written;
// a struct patch would leave findings stuck unread. Missing ids are
// ErrNotFound. Already-read is a no-op, not a 404.
func (s *Store) MarkRunRead(id string) error {
	now := time.Now().UTC()
	res := s.db.Model(&ScheduleRun{}).Where("id = ?", id).Updates(map[string]any{
		"unread":     false,
		"updated_at": now,
	})
	if res.Error != nil {
		return fmt.Errorf("store: mark schedule run read: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		if _, err := s.GetRun(id); err != nil {
			return err
		}
	}
	return nil
}

// CountUnreadRuns is the inbox badge: findings and errors, not quiet.
func (s *Store) CountUnreadRuns() (int, error) {
	var n int64
	if err := s.db.Model(&ScheduleRun{}).Where("unread = ?", true).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("store: count unread schedule runs: %w", err)
	}
	return int(n), nil
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

// ListSchedules returns every row, due or not. The inbox filters; the
// ticker uses ListDue.
func (s *Store) ListSchedules() ([]Schedule, error) {
	var out []Schedule
	err := s.db.Order("next_run_at asc, id asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list schedules: %w", err)
	}
	return out, nil
}

// CancelSchedule marks one wait cancelled if it is still active or paused.
// Already-cancelled (or done) rows return (nil, nil) so the engine does
// not write a second cancelled event. Missing ids are ErrNotFound.
func (s *Store) CancelSchedule(id string) (*Schedule, error) {
	now := time.Now().UTC()
	res := s.db.Model(&Schedule{}).
		Where("id = ? AND status IN ?", id, []string{ScheduleActive, SchedulePaused}).
		Updates(map[string]any{
			"status":     ScheduleCancelled,
			"updated_at": now,
		})
	if res.Error != nil {
		return nil, fmt.Errorf("store: cancel schedule: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		if _, err := s.GetSchedule(id); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return s.GetSchedule(id)
}

// ResumeScheduleUnderCap flips a paused row to active only if CountActive
// is still below cap. Already-active is a no-op so a row does not count
// against itself. Count and update share one transaction.
func (s *Store) ResumeScheduleUnderCap(id string, cap int) (*Schedule, error) {
	var out *Schedule
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row Schedule
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("store: get schedule: %w", err)
		}
		if row.Status == ScheduleActive {
			out = &row
			return nil
		}
		if row.Status != SchedulePaused {
			return fmt.Errorf("store: schedule is not paused")
		}
		n, err := countActive(tx)
		if err != nil {
			return err
		}
		if n >= cap {
			return ErrScheduleCap
		}
		now := time.Now().UTC()
		// Same transaction as the count, and this process has one SQLite
		// connection: the row we just saw paused cannot change under us.
		if err := tx.Model(&Schedule{}).Where("id = ?", id).Updates(map[string]any{
			"status":     ScheduleActive,
			"updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("store: update schedule: %w", err)
		}
		row.Status = ScheduleActive
		row.UpdatedAt = now
		out = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ActivateDoneUnderCap rearms a finished one-shot. Count and the status
// flip share one transaction so a parallel create cannot sneak past the cap.
// Cancelled and paused rows stay dead — next_in_s is not an undo.
func (s *Store) ActivateDoneUnderCap(id string, cap int, fields map[string]any) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row Schedule
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("store: get schedule: %w", err)
		}
		if row.Status != ScheduleDone {
			return fmt.Errorf("store: schedule is not done")
		}
		n, err := countActive(tx)
		if err != nil {
			return err
		}
		if n >= cap {
			return ErrScheduleCap
		}
		patch := map[string]any{}
		for k, v := range fields {
			patch[k] = v
		}
		patch["status"] = ScheduleActive
		patch["updated_at"] = time.Now().UTC()
		res := tx.Model(&Schedule{}).Where("id = ? AND status = ?", id, ScheduleDone).Updates(patch)
		if res.Error != nil {
			return fmt.Errorf("store: update schedule: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("store: schedule is not done")
		}
		return nil
	})
}

// UpdateSchedule applies a field patch. Unknown ids report ErrNotFound
// rather than silently doing nothing.
func (s *Store) UpdateSchedule(id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()
	res := s.db.Model(&Schedule{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return fmt.Errorf("store: update schedule: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateActiveSchedule patches an armed row. Zero rows means it is no
// longer active (cancelled, done, paused) — the ticker must stop, not
// resurrect it by writing status=active.
func (s *Store) UpdateActiveSchedule(id string, fields map[string]any) (bool, error) {
	if len(fields) == 0 {
		return true, nil
	}
	fields["updated_at"] = time.Now().UTC()
	res := s.db.Model(&Schedule{}).Where("id = ? AND status = ?", id, ScheduleActive).Updates(fields)
	if res.Error != nil {
		return false, fmt.Errorf("store: update active schedule: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// BindRun writes the conversation and turn a claimed fire landed on.
func (s *Store) BindRun(id, threadID, turnID string) error {
	now := time.Now().UTC()
	fields := map[string]any{"updated_at": now}
	if threadID != "" {
		fields["thread_id"] = threadID
	}
	if turnID != "" {
		fields["turn_id"] = turnID
	}
	res := s.db.Model(&ScheduleRun{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return fmt.Errorf("store: bind schedule run: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// cancelSchedulesForProject marks standalone jobs pinned to this project
// cancelled. Origin-only rows with an empty project_id stay: they are not
// bound to this workspace.
func cancelSchedulesForProject(db *gorm.DB, projectID string) error {
	if projectID == "" {
		return nil
	}
	now := time.Now().UTC()
	return db.Model(&Schedule{}).
		Where("project_id = ? AND status IN ?", projectID, []string{ScheduleActive, SchedulePaused}).
		Updates(map[string]any{
			"status":     ScheduleCancelled,
			"updated_at": now,
		}).Error
}
