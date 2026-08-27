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

package logging

import (
	"context"
	"log/slog"
	"time"
)

const (
	TransportCompletedEvent   = "transport.request.completed"
	ApplicationCompletedEvent = "application.operation.completed"
)

// TransportObservation is the bounded, content-free completion state for one
// externally visible transport request. StatusCode is omitted when it is zero,
// which is used for cancellation before a response starts.
type TransportObservation struct {
	Operation  string
	Outcome    string
	Transport  string
	Duration   time.Duration
	StatusCode int
}

// LogTransportCompletion emits the frozen transport completion event. Client
// failures remain informational; only a completed 5xx response is an error.
func LogTransportCompletion(ctx context.Context, logger *slog.Logger, value TransportObservation) {
	if logger == nil {
		return
	}
	attributes := []slog.Attr{
		slog.String("event", TransportCompletedEvent),
		slog.String("operation", value.Operation),
		slog.String("outcome", value.Outcome),
		slog.String("transport", value.Transport),
		slog.String("unit", "transport"),
		slog.Float64("duration_ms", durationMilliseconds(value.Duration)),
	}
	level := slog.LevelInfo
	if value.StatusCode > 0 {
		attributes = append(attributes, slog.Int("status_code", value.StatusCode))
		if value.StatusCode >= 500 {
			level = slog.LevelError
		}
	}
	LogSafely(ctx, logger, level, "PowerContext transport request completed", attributes...)
}

// ApplicationObservation describes an application failure or cancellation.
// Successful operations are represented by metrics and spans but intentionally
// do not produce an extra log record, matching the frozen Server behavior.
type ApplicationObservation struct {
	Operation  string
	Outcome    string
	Duration   time.Duration
	StatusCode int
	ErrorCode  string
}

func LogApplicationCompletion(ctx context.Context, logger *slog.Logger, value ApplicationObservation) {
	if logger == nil {
		return
	}
	var level slog.Level
	var message string
	switch value.Outcome {
	case "cancelled":
		level = slog.LevelInfo
		message = "PowerContext application operation cancelled"
	case "failure":
		level = slog.LevelWarn
		if value.StatusCode >= 500 {
			level = slog.LevelError
		}
		message = "PowerContext application operation failed"
	default:
		return
	}
	attributes := []slog.Attr{
		slog.String("event", ApplicationCompletedEvent),
		slog.String("operation", value.Operation),
		slog.String("outcome", value.Outcome),
		slog.String("unit", "application"),
		slog.Float64("duration_ms", durationMilliseconds(value.Duration)),
	}
	if value.ErrorCode != "" {
		attributes = append(attributes, slog.String("error_code", value.ErrorCode))
	}
	LogSafely(ctx, logger, level, message, attributes...)
}

// LogLifecycle emits one bounded lifecycle transition without attaching
// arbitrary configuration or dependency errors.
func LogLifecycle(ctx context.Context, logger *slog.Logger, event, message string) {
	if logger == nil {
		return
	}
	LogSafely(ctx, logger, slog.LevelInfo, message,
		slog.String("event", event),
		slog.String("unit", "server"),
	)
}

// LogSafely ensures observability remains a side effect: even a custom handler
// or writer that panics cannot change an application response or lifecycle.
func LogSafely(ctx context.Context, logger *slog.Logger, level slog.Level, message string, attributes ...slog.Attr) {
	if logger == nil {
		return
	}
	defer func() { _ = recover() }()
	logger.LogAttrs(ctx, level, message, attributes...)
}

func durationMilliseconds(value time.Duration) float64 {
	if value < 0 {
		return 0
	}
	return float64(value) / float64(time.Millisecond)
}
