package models

import (
	"encoding/json"
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuration_Set(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Duration
		wantErr bool
	}{
		{"valid seconds", "10s", Duration(10 * time.Second), false},
		{"valid minutes", "5m", Duration(5 * time.Minute), false},
		{"valid hours", "2h", Duration(2 * time.Hour), false},
		{"empty string", "", Duration(0), false},
		{"invalid format", "invalid", Duration(0), true},
		{"negative", "-5s", Duration(-5 * time.Second), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.Set(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want.Duration(), d.Duration())
			}
		})
	}
}

func TestDuration_String(t *testing.T) {
	tests := []struct {
		input Duration
		want  string
	}{
		{Duration(10 * time.Second), "10s"},
		{Duration(5 * time.Minute), "5m0s"},
		{Duration(2 * time.Hour), "2h0m0s"},
		{Duration(0), "0s"},
		{Duration(-5 * time.Second), "-5s"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.input.String())
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    Duration
		wantErr bool
	}{
		{"string seconds", []byte(`"10s"`), Duration(10 * time.Second), false},
		{"string minutes", []byte(`"5m"`), Duration(5 * time.Minute), false},
		{"string hours", []byte(`"2h"`), Duration(2 * time.Hour), false},
		{"number seconds", []byte(`30`), Duration(30 * time.Second), false},
		{"float seconds", []byte(`30.5`), Duration(30 * time.Second), false},
		{"invalid string", []byte(`"invalid"`), Duration(0), true},
		{"invalid number", []byte(`{"bad":"json"}`), Duration(0), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal(tt.input, &d)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want.Duration(), d.Duration())
			}
		})
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		input Duration
		want  string
	}{
		{Duration(10 * time.Second), `"10s"`},
		{Duration(5 * time.Minute), `"5m0s"`},
		{Duration(2 * time.Hour), `"2h0m0s"`},
		{Duration(0), `"0s"`},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

func TestDuration_UnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    Duration
		wantErr bool
	}{
		{"valid seconds", []byte("10s"), Duration(10 * time.Second), false},
		{"valid minutes", []byte("5m"), Duration(5 * time.Minute), false},
		{"empty", []byte(""), Duration(0), false},
		{"invalid", []byte("invalid"), Duration(0), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalText(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want.Duration(), d.Duration())
			}
		})
	}
}

func TestDuration_MarshalText(t *testing.T) {
	tests := []struct {
		input Duration
		want  string
	}{
		{Duration(10 * time.Second), "10s"},
		{Duration(5 * time.Minute), "5m0s"},
		{Duration(2 * time.Hour), "2h0m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := tt.input.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

func TestDuration_Duration(t *testing.T) {
	d := Duration(10 * time.Second)
	assert.Equal(t, 10*time.Second, d.Duration())
}

func TestDuration_FlagIntegration(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var d Duration
	fs.Var(&d, "timeout", "timeout duration")

	t.Run("valid flag", func(t *testing.T) {
		err := fs.Parse([]string{"-timeout", "30s"})
		require.NoError(t, err)
		assert.Equal(t, 30*time.Second, d.Duration())
	})

	t.Run("invalid flag", func(t *testing.T) {
		err := fs.Parse([]string{"-timeout", "invalid"})
		assert.Error(t, err)
	})

	t.Run("empty flag", func(t *testing.T) {
		fs2 := flag.NewFlagSet("test", flag.ContinueOnError)
		var d2 Duration
		fs2.Var(&d2, "timeout", "timeout")
		err := fs2.Parse([]string{"-timeout", ""})
		require.NoError(t, err)
		assert.Equal(t, 0*time.Second, d2.Duration())
	})
}

func TestDuration_RoundTripJSON(t *testing.T) {
	tests := []struct {
		name string
		val  Duration
	}{
		{"5 seconds", Duration(5 * time.Second)},
		{"2.5 seconds", Duration(2500 * time.Millisecond)},
		{"10 minutes", Duration(10 * time.Minute)},
		{"0", Duration(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.val)
			require.NoError(t, err)

			var d Duration
			err = json.Unmarshal(data, &d)
			require.NoError(t, err)
			assert.Equal(t, tt.val.Duration(), d.Duration())
		})
	}
}
