package main

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// BuildKit prints a line per downloaded chunk ("#5 sha256:abc 1.05MB /
// 15.24MB 0.8s"); only the final "done" line is worth keeping.
var buildProgressRe = regexp.MustCompile(`^#\d+ sha256:[0-9a-f]+ [\d.]+[kMG]?B / [\d.]+[kMG]?B [\d.]+s$`)

// logShipper batches deployment log lines and sends them to the backend
// twice a second, so the dashboard can show progress live.
type logShipper struct {
	api          *API
	deploymentID string
	revisionID   string

	mu      sync.Mutex
	pending []LogEntry
	done    chan struct{}
	stopped chan struct{}
}

func (a *Agent) newLogShipper(deploymentID, revisionID string) *logShipper {
	l := &logShipper{api: a.api, deploymentID: deploymentID, revisionID: revisionID,
		done: make(chan struct{}), stopped: make(chan struct{})}
	go l.loop()
	return l
}

func (l *logShipper) add(stream, text string) {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return
	}
	l.mu.Lock()
	l.pending = append(l.pending, LogEntry{Stream: stream, Text: text})
	l.mu.Unlock()
}

// Build records output from image builds; Deploy records Insta Deploy's
// own progress messages.
func (l *logShipper) Build(text string) {
	if !buildProgressRe.MatchString(strings.TrimSpace(text)) {
		l.add("build", text)
	}
}
func (l *logShipper) Deploy(text string) { l.add("deploy", text) }

func (l *logShipper) loop() {
	defer close(l.stopped)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			l.flush()
		case <-l.done:
			l.flush()
			return
		}
	}
}

func (l *logShipper) flush() {
	l.mu.Lock()
	batch := l.pending
	l.pending = nil
	l.mu.Unlock()
	for len(batch) > 0 {
		n := min(len(batch), 500)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := l.api.SendLogs(ctx, l.deploymentID, l.revisionID, batch[:n]); err != nil {
			log.Printf("send logs: %v", err)
		}
		cancel()
		batch = batch[n:]
	}
}

// Close sends what's left and stops the shipper.
func (l *logShipper) Close() {
	close(l.done)
	<-l.stopped
}
