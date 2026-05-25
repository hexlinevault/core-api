package bootstrap

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/robfig/cron/v3"
)

// RegisterCronjob registers cronjob. Note: Concurrency control is now managed by StartCronLeaderElection.
func RegisterCronjob(c *cron.Cron, name string, t string, f func(), connectionNames ...string) {
	cronjob := strings.Split(t, ",")
	for _, ct := range cronjob {
		ct = strings.TrimSpace(ct)
		_, err := c.AddFunc(ct, f)
		if err != nil {
			Logger(context.Background()).
				WithError(err).
				WithField("cronjob", name).
				WithField("schedule", ct).
				Errorf("Failed to register cronjob %s with schedule %s", name, ct)
		}
	}
}

// StartCronLeaderElection starts a blocking leader election loop.
// It acquires a distributed lock, and when successful, starts the cron scheduler ensuring single-instance execution.
func StartCronLeaderElection(leaderKey string, registerJobs func(c *cron.Cron), connectionNames ...string) {
	ctx := context.Background()
	go func() {
		cronDebug := os.Getenv("CRON_DEBUG") == "true"
		leaderLockDuration := 10 * time.Second

		// Ensure we have a Redis connection/RedSync instance
		rs := new(Redis).RedSync(connectionNames...)
		if rs == nil {
			Logger(ctx).Error("RedSync not initialized, cannot start cron leader election")
			return
		}

		for {
			// Try to acquire the leader lock
			// Fail fast (Try 1) because we are in a loop
			mutex := rs.NewMutex(leaderKey, redsync.WithExpiry(leaderLockDuration), redsync.WithTries(1))

			if err := mutex.Lock(); err != nil {
				// Not leader
				if cronDebug {
					Logger(ctx).Info("Candidate is not leader, waiting...")
				}
				time.Sleep(leaderLockDuration / 2)
				continue
			}

			// This instance is now the leader
			Logger(ctx).Info("Acquired leader lock, starting cron scheduler...")

			// Create and start scheduler
			cronScheduler := cron.New(cron.WithSeconds())
			registerJobs(cronScheduler)
			cronScheduler.Start()

			// Maintain leadership
			lostLeadership := false
			ticker := time.NewTicker(leaderLockDuration / 2)

			// Inner loop: Refresh lock
		RefreshLoop:
			for {
				select {
				case <-ctx.Done():
					// Application shutdown
					lostLeadership = true // Force stop
					break RefreshLoop
				case <-ticker.C:
					if ok, err := mutex.Extend(); !ok || err != nil {
						Logger(ctx).WithError(err).Error("Failed to extend leader lock, stopping scheduler...")
						lostLeadership = true
						break RefreshLoop
					}
					Logger(ctx).Debug("Leader lock extended")
				}
			}

			ticker.Stop()
			cronScheduler.Stop() // Stop all jobs immediately
			Logger(ctx).Info("Cron scheduler stopped")

			if !lostLeadership {
				// If we exited voluntarily (e.g. ctx done), try to unlock to help next leader
				if _, err := mutex.Unlock(); err != nil {
					Logger(ctx).WithError(err).Warn("Failed to release leader lock")
				}
			}

			if ctx.Err() != nil {
				return // Exit if context cancelled
			}

			// Wait a bit before retrying election
			time.Sleep(1 * time.Second)
		}
	}()
}
