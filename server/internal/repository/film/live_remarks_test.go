package film

import "testing"

func TestPickLiveRemarkMasterLeadsUsesMasterText(t *testing.T) {
	got := pickLiveRemark(120, "更新至120集", "第119集", 120)
	if got != "更新至120集" {
		t.Fatalf("主站未落后应用主站文案, got %q", got)
	}
}

func TestPickLiveRemarkSlaveAheadUsesLeadingProgress(t *testing.T) {
	got := pickLiveRemark(119, "更新至119集", "第120集", 120)
	if got != "更新至第120集" {
		t.Fatalf("附属站先追集应对齐最大集数, got %q", got)
	}
}

func TestPickLiveRemarkFinishedLabel(t *testing.T) {
	got := pickLiveRemark(10, "更新至10集", "完结", 12)
	if got != "完结" {
		t.Fatalf("先到完结应展示完结, got %q", got)
	}
}

func TestPickLiveRemarkNoEpisodesFallsBack(t *testing.T) {
	got := pickLiveRemark(0, "正片", "", 0)
	if got != "正片" {
		t.Fatalf("无线路应回落主站文案, got %q", got)
	}
}

func TestFormatLiveRemark(t *testing.T) {
	if got := formatLiveRemark("", 20); got != "更新至20集" {
		t.Fatalf("got %q", got)
	}
	if got := formatLiveRemark("HD", 1); got != "HD" {
		t.Fatalf("got %q", got)
	}
}
