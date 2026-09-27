package residue

import (
	"testing"
	"time"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

func testNetwork() ([]route.Segment, map[string]model.Station) {
	network := route.DefaultNetwork()
	stations := make(map[string]model.Station)
	for _, station := range network.Stations() {
		stations[station.ID] = station
	}
	return network.Segments(), stations
}

func testSample(runID, key, station string, chainage float64, wax float64) model.ResidueSample {
	return model.ResidueSample{
		ID:         key + "-id",
		RunID:      runID,
		SampleKey:  key,
		StationID:  station,
		ChainageKM: chainage,
		Composition: model.ResidueComposition{
			WaxPercent:    wax,
			SandPercent:   100 - wax,
			DebrisPercent: 0,
			OtherPercent:  0,
		},
		CollectedAt: time.Now().UTC(),
	}
}

func TestSegmentForChainage(t *testing.T) {
	segments, stations := testNetwork()
	segment, err := SegmentForChainage(segments, stations, 40)
	if err != nil || segment.ID != "segment-a-b" {
		t.Fatalf("expected segment-a-b, got %v %v", segment.ID, err)
	}
	segment, err = SegmentForChainage(segments, stations, 173.2)
	if err != nil || segment.ID != "segment-b-c" {
		t.Fatalf("expected receiver chainage to attribute to segment-b-c, got %v %v", segment.ID, err)
	}
	if _, err = SegmentForChainage(segments, stations, 500); err == nil {
		t.Fatal("expected out-of-network chainage to fail")
	}
}

func TestEffectiveMassMedian(t *testing.T) {
	archive := model.ResidueArchive{
		RunID: "run-1",
		Weighings: []model.ResidueWeighing{
			{SampleKey: "S-1", MassKg: 10.0},
			{SampleKey: "S-1", MassKg: 10.4},
			{SampleKey: "S-1", MassKg: 40.0},
			{SampleKey: "S-2", MassKg: 5.0},
		},
	}
	mass, ok := EffectiveMass(archive, "S-1")
	if !ok || mass != 10.4 {
		t.Fatalf("expected median 10.4, got %v", mass)
	}
	mass, ok = EffectiveMass(archive, "S-2")
	if !ok || mass != 5.0 {
		t.Fatalf("expected single weighing 5.0, got %v", mass)
	}
	if _, ok = EffectiveMass(archive, "S-3"); ok {
		t.Fatal("expected missing sample to have no effective mass")
	}
	repeats := RepeatWeighingKeys(archive)
	if repeats["S-1"] != 3 {
		t.Fatalf("expected S-1 flagged with 3 weighings, got %v", repeats)
	}
	if _, ok := repeats["S-2"]; ok {
		t.Fatal("expected S-2 to not be flagged as repeat weighed")
	}
}

func TestAnalyzeSegmentTrend(t *testing.T) {
	segments, stations := testNetwork()
	archive := model.ResidueArchive{
		RunID: "run-1",
		Samples: []model.ResidueSample{
			testSample("run-1", "S-1", "station-a", 40, 60),
			testSample("run-1", "S-2", "station-b", 120, 20),
		},
		Weighings: []model.ResidueWeighing{
			{SampleKey: "S-1", MassKg: 8.0},
			{SampleKey: "S-1", MassKg: 8.4},
			{SampleKey: "S-2", MassKg: 60.0},
		},
	}
	analysis, err := Analyze(archive, segments, stations)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(analysis.Segments) != 2 {
		t.Fatalf("expected 2 segment profiles, got %d", len(analysis.Segments))
	}
	first, second := analysis.Segments[0], analysis.Segments[1]
	if first.SegmentID != "segment-a-b" || second.SegmentID != "segment-b-c" {
		t.Fatalf("unexpected segment order %s, %s", first.SegmentID, second.SegmentID)
	}
	if first.TotalMassKg != 8.2 {
		t.Fatalf("expected repeat weighings collapsed to median 8.2, got %.2f", first.TotalMassKg)
	}
	if first.Direction != DirectionIsolated {
		t.Fatalf("expected first segment isolated, got %s", first.Direction)
	}
	if second.Direction != DirectionIncreasing {
		t.Fatalf("expected downstream segment increasing, got %s", second.Direction)
	}
	if first.WaxPercent != 60 || second.WaxPercent != 20 {
		t.Fatalf("unexpected weighted composition %.1f / %.1f", first.WaxPercent, second.WaxPercent)
	}
	if len(analysis.SampleIDs) != 2 {
		t.Fatalf("expected analysis to reference both samples, got %v", analysis.SampleIDs)
	}
}

func TestAnalyzeRejectsUnweighedArchive(t *testing.T) {
	segments, stations := testNetwork()
	archive := model.ResidueArchive{
		RunID:   "run-1",
		Samples: []model.ResidueSample{testSample("run-1", "S-1", "station-a", 40, 60)},
	}
	if _, err := Analyze(archive, segments, stations); err == nil {
		t.Fatal("expected analysis without weighings to fail")
	}
}

func TestStoreDuplicateSampleKey(t *testing.T) {
	store := NewStore()
	if err := store.AddSample(testSample("run-1", "S-1", "station-a", 40, 60)); err != nil {
		t.Fatalf("add sample: %v", err)
	}
	if err := store.AddSample(testSample("run-1", "S-1", "station-a", 41, 60)); err == nil {
		t.Fatal("expected duplicate sample key to be rejected")
	}
	if err := store.AddWeighing(model.ResidueWeighing{RunID: "run-1", SampleKey: "S-9", MassKg: 1, ScaleID: "scale-1"}); err == nil {
		t.Fatal("expected weighing for unknown sample to be rejected")
	}
	if err := store.AddWeighing(model.ResidueWeighing{RunID: "run-1", SampleKey: "S-1", MassKg: 9.5, ScaleID: "scale-1"}); err != nil {
		t.Fatalf("add weighing: %v", err)
	}
}

func TestStoreAnalysisGenerationAndRestore(t *testing.T) {
	store := NewStore()
	first := store.AppendAnalysis("run-1", model.ResidueAnalysis{RunID: "run-1"})
	second := store.AppendAnalysis("run-1", model.ResidueAnalysis{RunID: "run-1"})
	if first.Generation != 1 || second.Generation != 2 {
		t.Fatalf("expected generations 1,2 got %d,%d", first.Generation, second.Generation)
	}
	restored := NewStore()
	restored.Restore(store.SnapshotAll())
	third := restored.AppendAnalysis("run-1", model.ResidueAnalysis{RunID: "run-1"})
	if third.Generation != 3 {
		t.Fatalf("expected generation to resume at 3, got %d", third.Generation)
	}
}
