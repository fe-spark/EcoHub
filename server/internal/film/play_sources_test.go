package film

import (
	"testing"

	"server/internal/model"
)

func TestDisambiguateManualPlayNames(t *testing.T) {
	lines := []model.PlayLinkVo{
		{Id: "manual#0", SourceId: "manual", Name: "自定义线路", IsManual: true},
		{Id: "manual#1", SourceId: "manual", Name: "自定义线路", IsManual: true},
		{Id: "manual#2", SourceId: "manual", Name: "专属直连", IsManual: true},
		{Id: "src_a#0", SourceId: "src_a", Name: "自定义线路", IsManual: false},
	}

	disambiguateManualPlayNames(lines)

	if lines[0].Name != "自定义线路-1" {
		t.Fatalf("expected manual#0 to be 自定义线路-1, got %q", lines[0].Name)
	}
	if lines[1].Name != "自定义线路-2" {
		t.Fatalf("expected manual#1 to be 自定义线路-2, got %q", lines[1].Name)
	}
	if lines[2].Name != "专属直连" {
		t.Fatalf("expected unique manual line name unchanged, got %q", lines[2].Name)
	}
	if lines[3].Name != "自定义线路" {
		t.Fatalf("expected non-manual line name unchanged, got %q", lines[3].Name)
	}
}
