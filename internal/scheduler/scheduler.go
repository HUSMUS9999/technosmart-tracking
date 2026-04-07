package scheduler

import (
	"fmt"
	"log"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler wraps robfig/cron for job management.
type Scheduler struct {
	cron *cron.Cron
}

// New creates a scheduler with the given timezone.
func New(timezone string) (*Scheduler, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	c := cron.New(cron.WithLocation(loc), cron.WithSeconds())
	return &Scheduler{cron: c}, nil
}

// AddJob adds a scheduled job. spec is a 6-field cron expression (sec min hour dom mon dow).
func (s *Scheduler) AddJob(name, spec string, fn func()) error {
	id, err := s.cron.AddFunc(spec, fn)
	if err != nil {
		return fmt.Errorf("add job %q: %w", name, err)
	}
	log.Printf("[scheduler] Added job %q (id=%d) schedule=%q", name, id, spec)
	return nil
}

// Start begins executing scheduled jobs.
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Println("[scheduler] Started")
}

// Stop halts the scheduler gracefully.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("[scheduler] Stopped")
}
