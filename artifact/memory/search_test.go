package memory

import (
	"math"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
)

func channelHit(t *testing.T, memoryID, entryID, text string, distance *float64) ChannelHit {
	t.Helper()
	ref, err := artifact.NewRef(Family, memoryID, 2)
	if err != nil {
		t.Fatal(err)
	}
	hit, err := NewChannelHit(ref, entryID, "version-"+entryID, text, distance)
	if err != nil {
		t.Fatal(err)
	}
	return hit
}

func TestAnalyzeTextUsesWordsAndCJKUnigramsBigrams(t *testing.T) {
	got := AnalyzeText("Café_API，记忆库")
	want := "café_api u_8bb0 u_5fc6 u_5e93 b_8bb0_5fc6 b_5fc6_5e93"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFTSAdmissionRejectsSingleCommonTermButKeepsSpecificOverlap(t *testing.T) {
	text := "Use PostgreSQL advisory locks for leader election."
	if AdmitsFTSText("Should we use blue icons in the mobile navigation bar?", text) {
		t.Fatal("unrelated common term was admitted")
	}
	if !AdmitsFTSText("Which locks should leader election use?", text) {
		t.Fatal("specific overlap was rejected")
	}
}

func TestFTSAdmissionKeepsOneTermQueriesUsable(t *testing.T) {
	if !AdmitsFTSText("atomic", "Use one atomic composition boundary.") {
		t.Fatal("one-term query became unusable")
	}
}

func TestSingleChannelUsesPublicRRFScoreAndInputRankOrder(t *testing.T) {
	zulu := channelHit(t, "memory-z", "zulu", "zulu", nil)
	alpha := channelHit(t, "memory-a", "alpha", "alpha", nil)
	hits, err := FuseRankings([]ChannelHit{zulu, alpha}, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].EntryID != "zulu" || hits[1].EntryID != "alpha" {
		t.Fatalf("single-channel rank order = %#v", hits)
	}
	if math.Abs(hits[0].Score-1.0/61) > 1e-15 || math.Abs(hits[1].Score-1.0/62) > 1e-15 {
		t.Fatalf("public RRF scores = %v, %v", hits[0].Score, hits[1].Score)
	}
	for _, hit := range hits {
		if len(hit.MatchedBy) != 1 || hit.MatchedBy[0] != MatchedFTS {
			t.Fatalf("single-channel matched_by = %v", hit.MatchedBy)
		}
	}
}

func TestDuplicateChannelRowsContributeOnlyFirstRank(t *testing.T) {
	alpha := channelHit(t, "memory-a", "alpha", "alpha", nil)
	hits, err := FuseRankings([]ChannelHit{alpha, alpha}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || math.Abs(hits[0].Score-1.0/61) > 1e-15 {
		t.Fatalf("duplicate row changed RRF contribution: %#v", hits)
	}
}

func TestFuseRankingsRequiresPositiveLimit(t *testing.T) {
	if _, err := FuseRankings(nil, nil, 0); err == nil {
		t.Fatal("zero limit was accepted")
	}
}

func TestRRFUsesStableIdentityTieBreak(t *testing.T) {
	alpha := channelHit(t, "memory-a", "alpha", "alpha", nil)
	beta := channelHit(t, "memory-a", "beta", "beta", nil)
	hits, err := FuseRankings([]ChannelHit{alpha, beta}, []ChannelHit{beta, alpha}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].EntryID != "alpha" || hits[1].EntryID != "beta" {
		t.Fatalf("unstable tie order: %v", hits)
	}
	want := 1.0/61 + 1.0/62
	if math.Abs(hits[0].Score-want) > 1e-15 || len(hits[0].MatchedBy) != 2 {
		t.Fatalf("unexpected fused hit: %#v", hits[0])
	}
}

func TestVectorAdmissionUsesUnitL2CosineThreshold(t *testing.T) {
	boundary := math.Sqrt(2 * (1 - 0.3))
	rejectedDistance := boundary + 0.001
	accepted := channelHit(t, "memory-a", "accepted", "", &boundary)
	rejected := channelHit(t, "memory-a", "rejected", "", &rejectedDistance)
	missing := channelHit(t, "memory-a", "missing", "", nil)
	got := AdmitVectorCandidates([]ChannelHit{accepted, rejected, missing})
	if len(got) != 1 || got[0].EntryID != "accepted" {
		t.Fatalf("unexpected admitted hits: %v", got)
	}
}
