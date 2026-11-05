package spider

import "testing"

func sealJob(id string, n int) sealSiteJob {
	return sealSiteJob{sourceID: id, mids: make([]int64, n)}
}

func TestCoalesceSealJobs_StopsBeforeOverflow(t *testing.T) {
	jobs := []sealSiteJob{
		sealJob("a", 100),
		sealJob("b", 19000),
		sealJob("c", 5000),
		sealJob("d", 25000),
	}
	taken, rest := coalesceSealJobs(jobs, sealCoalesceMax)
	if len(taken) != 2 || taken[0].sourceID != "a" || taken[1].sourceID != "b" {
		t.Fatalf("first take = %+v", taken)
	}
	if got := len(taken[0].mids) + len(taken[1].mids); got != 19100 {
		t.Fatalf("first take mids = %d", got)
	}
	if len(rest) != 2 || rest[0].sourceID != "c" {
		t.Fatalf("rest = %+v", rest)
	}

	taken, rest = coalesceSealJobs(rest, sealCoalesceMax)
	if len(taken) != 1 || taken[0].sourceID != "c" || len(rest) != 1 || rest[0].sourceID != "d" {
		t.Fatalf("second take=%+v rest=%+v", taken, rest)
	}

	taken, rest = coalesceSealJobs(rest, sealCoalesceMax)
	if len(taken) != 1 || taken[0].sourceID != "d" || len(rest) != 0 {
		t.Fatalf("oversized site take=%+v rest=%+v", taken, rest)
	}
}

func TestSealQueue_ArrivalStaysForNextTake(t *testing.T) {
	q := &searchResealQueue{
		sealing: []sealSiteJob{sealJob("a", 1)},
		pending: []sealSiteJob{sealJob("b", 100)},
	}
	q.pending = append(q.pending, sealJob("c", 1))
	if len(q.sealing) != 1 || q.sealing[0].sourceID != "a" {
		t.Fatalf("current seal changed: %+v", q.sealing)
	}
	if len(q.pending) != 2 || q.pending[1].sourceID != "c" {
		t.Fatalf("pending = %+v", q.pending)
	}
}

func TestAbsorbPendingSameSource_LatestKeepsEarlierMIDs(t *testing.T) {
	pending := []sealSiteJob{
		{sourceID: "a", mids: []int64{1, 2}},
		{sourceID: "b", mids: []int64{9}},
		{sourceID: "a", mids: []int64{2, 3}},
	}
	kept, job := absorbPendingSameSource(pending, sealSiteJob{sourceID: "a", mids: []int64{3, 4}})
	if len(kept) != 1 || kept[0].sourceID != "b" {
		t.Fatalf("kept = %+v", kept)
	}
	if got := job.mids; len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("merged mids = %v", got)
	}
	pending = append(kept, job)
	sites := sealQueueSites(nil, pending)
	if len(sites) != 2 || sites[0].SourceID != "b" || sites[0].Ahead != 0 || sites[1].SourceID != "a" || sites[1].Ahead != 1 {
		t.Fatalf("sites = %+v", sites)
	}
}

func TestAbsorbPendingSameSource_LeavesSealingJob(t *testing.T) {
	sealing := []sealSiteJob{{sourceID: "a", mids: []int64{7}}}
	pending := []sealSiteJob{{sourceID: "a", mids: []int64{1}}, {sourceID: "b", mids: []int64{8}}}
	kept, job := absorbPendingSameSource(pending, sealSiteJob{sourceID: "a", mids: []int64{2}})
	if len(sealing) != 1 || sealing[0].mids[0] != 7 {
		t.Fatalf("sealing changed: %+v", sealing)
	}
	if len(kept) != 1 || kept[0].sourceID != "b" {
		t.Fatalf("kept = %+v", kept)
	}
	if len(job.mids) != 2 || job.mids[0] != 1 || job.mids[1] != 2 {
		t.Fatalf("merged = %v", job.mids)
	}
	sites := sealQueueSites(sealing, append(kept, job))
	if len(sites) != 3 || sites[0].State != "sealing" || sites[2].SourceID != "a" || sites[2].Ahead != 2 {
		t.Fatalf("sites = %+v", sites)
	}
}

func TestSealQueueSites_AnonymousSealCountsAhead(t *testing.T) {
	sites := sealQueueSites(
		[]sealSiteJob{{mids: []int64{1}}},
		[]sealSiteJob{sealJob("a", 1), {mids: []int64{2}}, sealJob("b", 1)},
	)
	if len(sites) != 2 {
		t.Fatalf("sites = %+v", sites)
	}
	if sites[0].SourceID != "a" || sites[0].Ahead != 1 {
		t.Fatalf("first waiter = %+v", sites[0])
	}
	if sites[1].SourceID != "b" || sites[1].Ahead != 3 {
		t.Fatalf("second waiter = %+v", sites[1])
	}
}

func TestSealQueueSites_AheadCountsSealing(t *testing.T) {
	sites := sealQueueSites(
		[]sealSiteJob{sealJob("a", 1)},
		[]sealSiteJob{sealJob("b", 1), sealJob("c", 1)},
	)
	if len(sites) != 3 {
		t.Fatalf("sites = %+v", sites)
	}
	if sites[0].State != "sealing" || sites[0].Ahead != 0 {
		t.Fatalf("sealing = %+v", sites[0])
	}
	if sites[1].SourceID != "b" || sites[1].State != "waiting" || sites[1].Ahead != 1 {
		t.Fatalf("first waiter = %+v", sites[1])
	}
	if sites[2].SourceID != "c" || sites[2].Ahead != 2 {
		t.Fatalf("second waiter = %+v", sites[2])
	}
}
