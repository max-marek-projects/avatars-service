// Package models provides helper functions for time duration operations.
package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a wrapper around time.Duration that supports:
//   - JSON unmarshaling from strings (e.g., "5s", "10m") or numbers (seconds)
//   - JSON marshaling to human-readable strings (e.g., "5s")
//   - Flag parsing via flag.Value interface (e.g., -timeout=30s)
//   - Text unmarshaling for environment variables and other text sources
//
// It is intended for use in configuration structures where durations need to be
// specified in a human-friendly format.
type Duration time.Duration

// Set implements flag.Value interface.
// It parses a duration string (e.g., "10s", "5m", "2h") and stores the value.
// If the string is empty, it returns nil (no change).
//
// Parameters:
//   - s: the duration string to parse.
//
// Returns:
//   - error: nil on success, or an error if parsing fails.
func (d *Duration) Set(s string) error {
	if s == "" {
		return nil
	}
	val, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration value: %w", err)
	}
	*d = Duration(val)
	return nil
}

// String implements flag.Value interface.
// It returns the duration as a human-readable string (e.g., "5s", "10m").
//
// Returns:
//   - string: the string representation of the duration.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// UnmarshalJSON implements json.Unmarshaler.
// It supports both string (e.g., "5s", "10m") and numeric (seconds) formats.
//
// Parameters:
//   - b: the JSON byte slice to unmarshal.
//
// Returns:
//   - error: nil on success, or an error if parsing fails.
func (d *Duration) UnmarshalJSON(b []byte) error {
	// Try as string (e.g., "5s")
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		val, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("failed to parse duration value: %w", err)
		}
		*d = Duration(val)
		return nil
	}
	// Try as number (seconds)
	var sec float64
	if err := json.Unmarshal(b, &sec); err == nil {
		*d = Duration(time.Duration(sec) * time.Second)
		return nil
	}
	return fmt.Errorf("invalid duration format: %s", string(b))
}

// MarshalJSON implements json.Marshaler.
// It returns the duration as a human-readable string (e.g., "5s", "10m").
//
// Returns:
//   - []byte: the JSON-encoded bytes.
//   - error: nil on success, or an error if marshaling fails.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalText implements encoding.TextUnmarshaler for environment variables
// and other text-based sources.
//
// Parameters:
//   - text: the text bytes to parse.
//
// Returns:
//   - error: nil on success, or an error if parsing fails.
func (d *Duration) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	val, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("failed to parse duration value: %w", err)
	}
	*d = Duration(val)
	return nil
}

// MarshalText implements encoding.TextMarshaler.
// It returns the duration as a human-readable string.
//
// Returns:
//   - []byte: the text-encoded bytes.
//   - error: nil on success.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// Duration returns the underlying time.Duration value.
//
// Returns:
//   - time.Duration: the duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}
