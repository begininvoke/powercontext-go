// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scheduler

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

const (
	SourceWindowJobID         = "powercontext.memory.source-window.v1"
	ExperienceIncubationJobID = "powercontext.experience.incubation.v1"
	TableName                 = "powercontext_scheduler_jobs"
)

type JobKind string

const (
	SourceWindow         JobKind = "source-window"
	ExperienceIncubation JobKind = "experience-incubation"
)

type Job struct {
	kind        JobKind
	runtimePath string
	interval    time.Duration
	start       time.Time
	next        time.Time
}

func NewJob(kind JobKind, runtimePath string, interval time.Duration, start, next time.Time) (Job, error) {
	if _, _, _, err := jobIdentity(kind); err != nil {
		return Job{}, err
	}
	if runtimePath == "" || runtimePath == ":memory:" || !filepath.IsAbs(runtimePath) {
		return Job{}, fmt.Errorf("scheduler runtime path must be an absolute file path")
	}
	if interval <= 0 || interval%time.Microsecond != 0 {
		return Job{}, fmt.Errorf("scheduler interval must be positive with microsecond precision")
	}
	start, err := utcMicrosecond("start_date", start)
	if err != nil {
		return Job{}, err
	}
	next, err = utcMicrosecond("next_run_time", next)
	if err != nil {
		return Job{}, err
	}
	delta := next.UnixMicro() - start.UnixMicro()
	intervalMicros := interval.Microseconds()
	if delta < 0 || delta%intervalMicros != 0 {
		return Job{}, fmt.Errorf("scheduler next run time is not aligned to its interval")
	}
	return Job{kind: kind, runtimePath: runtimePath, interval: interval, start: start, next: next}, nil
}

func (j Job) Kind() JobKind           { return j.kind }
func (j Job) ID() string              { id, _, _, _ := jobIdentity(j.kind); return id }
func (j Job) RuntimePath() string     { return j.runtimePath }
func (j Job) Interval() time.Duration { return j.interval }
func (j Job) StartDate() time.Time    { return j.start }
func (j Job) NextRunTime() time.Time  { return j.next }

func (j Job) withNext(now time.Time) (Job, error) {
	now = now.UTC()
	if now.Before(j.next) {
		return j, nil
	}
	elapsedMicros := now.UnixMicro() - j.start.UnixMicro()
	step := elapsedMicros/j.interval.Microseconds() + 1
	nextMicros := j.start.UnixMicro() + step*j.interval.Microseconds()
	next := time.UnixMicro(nextMicros).UTC()
	return NewJob(j.kind, j.runtimePath, j.interval, j.start, next)
}

func jobIdentity(kind JobKind) (id, callable, name string, err error) {
	switch kind {
	case SourceWindow:
		return SourceWindowJobID,
			"powercontext.builtin.runtime.scheduler:dispatch_source_windows",
			"dispatch_source_windows", nil
	case ExperienceIncubation:
		return ExperienceIncubationJobID,
			"powercontext.builtin.runtime.scheduler:dispatch_experience_incubation",
			"dispatch_experience_incubation", nil
	default:
		return "", "", "", fmt.Errorf("unsupported scheduler job kind %q", kind)
	}
}

func kindForID(id string) (JobKind, bool) {
	switch id {
	case SourceWindowJobID:
		return SourceWindow, true
	case ExperienceIncubationJobID:
		return ExperienceIncubation, true
	default:
		return "", false
	}
}

func utcMicrosecond(field string, value time.Time) (time.Time, error) {
	if value.IsZero() {
		return time.Time{}, fmt.Errorf("scheduler %s must be configured", field)
	}
	value = value.UTC()
	if value.Nanosecond()%1_000 != 0 {
		return time.Time{}, fmt.Errorf("scheduler %s exceeds Python microsecond precision", field)
	}
	if value.Year() < 1 || value.Year() > 9999 {
		return time.Time{}, fmt.Errorf("scheduler %s is outside Python datetime range", field)
	}
	return value, nil
}

func unixTimestamp(value time.Time) float64 {
	return float64(value.Unix()) + float64(value.Nanosecond()/1_000)/1_000_000
}

func sameTimestamp(column float64, value time.Time) bool {
	return !math.IsNaN(column) && !math.IsInf(column, 0) && column == unixTimestamp(value)
}

func canonicalPath(path string) (string, error) {
	if path == "" || path == ":memory:" {
		return "", fmt.Errorf("scheduler_path must reference a file")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	parent, parentErr := filepath.EvalSymlinks(filepath.Dir(absolute))
	if parentErr == nil {
		return filepath.Join(parent, filepath.Base(absolute)), nil
	}
	return filepath.Clean(absolute), nil
}

func validStoredRuntimePath(stored, expected string) bool {
	return stored == expected && filepath.IsAbs(stored) && !strings.ContainsRune(stored, '\x00')
}
