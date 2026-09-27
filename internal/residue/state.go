package residue

import (
	"github.com/wyw14/solo-036/internal/model"
	"sync"
)

type runArchive struct {
	samples    []model.ResidueSample
	weighings  []model.ResidueWeighing
	analyses   []model.ResidueAnalysis
	generation int64
}

type Store struct {
	mu       sync.Mutex
	archives map[string]*runArchive
}

func NewStore() *Store { return &Store{archives: make(map[string]*runArchive)} }

func (s *Store) archive(runID string) *runArchive {
	archive, ok := s.archives[runID]
	if !ok {
		archive = &runArchive{}
		s.archives[runID] = archive
	}
	return archive
}
