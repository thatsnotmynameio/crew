package core

import (
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// statistics is what the model knows of the crew process it records in
// the statistics store, when it records statistics (KTD4).
type statistics struct {
	version string
	folder  string
	// process is the id of the recorded process, which the work it runs
	// points to; empty until Started.
	process crew.ProcessID
}

// statisticsInput applies in, an input about the statistics store.
func (s *step) statisticsInput(in statisticsInput) {
	switch in := in.(type) {
	case Started:
		s.started(in.Seed)
	case StatisticFailed:
		s.emit(StatisticNotRecorded{At: s.at, Statistic: in.Statistic, Reason: in.Reason})
	}
}

// started records the process, with an id minted from seed, once, when the
// model records statistics.
func (s *step) started(seed uuid.UUID) {
	st := s.m.statistics
	if st == nil || st.process != "" {
		return
	}
	st.process = crew.ProcessID(seed.String())
	s.command(RecordStatistic{Statistic: crew.Process{
		ID: st.process, Version: st.version, Folder: st.folder, Start: s.at,
	}})
}
