package config_test

import (
	"reflect"
	"testing"
)

// Without statistics:, crew records to the sqlite store, whose section is
// empty.
func TestLoadRecordsToSqliteByDefault(t *testing.T) {
	cfg := load(t, oneRule)
	if cfg.Statistics != "sqlite" {
		t.Errorf("Statistics = %q, want sqlite", cfg.Statistics)
	}
	var settings struct{}
	if err := cfg.StatisticsSection(&settings); err != nil {
		t.Errorf("StatisticsSection: %v", err)
	}
}

// Covers AE12: store: off turns recording off.
func TestLoadTurnsRecordingOff(t *testing.T) {
	if cfg := load(t, "statistics: {store: off}\n"+oneRule); cfg.Statistics != "" {
		t.Errorf("Statistics = %q, want recording off", cfg.Statistics)
	}
}

// The keys of statistics: besides store go to the store's adapter, which
// decodes them.
func TestLoadHandsTheStoreItsKeys(t *testing.T) {
	cfg := load(t, "statistics:\n  store: sqlite\n  path: x\n"+oneRule)
	var settings struct {
		Path string `yaml:"path"`
	}
	if err := cfg.StatisticsSection(&settings); err != nil {
		t.Fatalf("StatisticsSection: %v", err)
	}
	if settings.Path != "x" {
		t.Errorf("path = %q, want x", settings.Path)
	}
	var none struct{}
	assertErr(t, cfg.StatisticsSection(&none), sharedName+": statistics.path (line 3): unknown key")
}

// The later file's statistics: replaces the earlier one's whole.
func TestTheLocalFileTurnsRecordingOff(t *testing.T) {
	cfg := loadAll(t, "statistics: {store: sqlite}\n", oneRule, "statistics: {store: off}\n")
	if cfg.Statistics != "" {
		t.Errorf("Statistics = %q, want config.local.yaml's off", cfg.Statistics)
	}
}

func TestLoadRejectsInvalidStatistics(t *testing.T) {
	tests := []struct {
		name, body string
		want       []string
	}{
		{
			name: "a key beside store: off",
			body: "statistics:\n  store: off\n  path: x\n",
			want: []string{sharedName + ": statistics.path (line 3): store is off, so nothing reads this key"},
		},
		{
			name: "an empty store",
			body: "statistics:\n  store: \"\"\n",
			want: []string{sharedName + ": statistics.store (line 2): must name a store, or off"},
		},
		{
			name: "not a mapping",
			body: "statistics: [a]\n",
			want: []string{sharedName + ": statistics (line 1): must be a mapping"},
		},
		{
			name: "store not a name",
			body: "statistics: {store: [sqlite]}\n",
			want: []string{sharedName + ": statistics.store (line 1): cannot unmarshal !!seq into string"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := loadFilesErr(t, tt.body+oneRule, noFile)
			if !reflect.DeepEqual(lines, tt.want) {
				t.Errorf("error = %q, want %q", lines, tt.want)
			}
		})
	}
}
