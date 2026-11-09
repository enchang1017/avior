package query

import (
	"reflect"
	"testing"

	"github.com/enchang1017/avior/backend/internal/adapter/explainjson"
)

// What pruning left, per partitioned table: the partitions read out of how many
// there are. A scan of an ordinary table is not partitioning's to report, and a
// partition read twice is one partition.
func TestScansAreGroupedByTheirPartitionedTable(t *testing.T) {
	t.Parallel()
	scans := []explainjson.Scan{
		{Schema: "public", Name: "rounds_2026_09", Rows: 40},
		{Schema: "public", Name: "member", Rows: 7},
		{Schema: "public", Name: "rounds_2026_08", Rows: 30},
		{Schema: "public", Name: "rounds_2026_09", Rows: 2},
		{Schema: "audit", Name: "outbox_default", Rows: 0},
	}
	roots := map[[2]string]PartitionRoot{
		{"public", "rounds_2026_08"}: {Schema: "public", Name: "rounds", Leaves: 29},
		{"public", "rounds_2026_09"}: {Schema: "public", Name: "rounds", Leaves: 29},
		{"audit", "outbox_default"}:  {Schema: "audit", Name: "outbox", Leaves: 3},
	}
	want := []PartitionScan{
		{Schema: "audit", Name: "outbox", Total: 3, Scanned: []ScannedPartition{
			{Schema: "audit", Name: "outbox_default", EstimatedRows: 0},
		}},
		{Schema: "public", Name: "rounds", Total: 29, Scanned: []ScannedPartition{
			{Schema: "public", Name: "rounds_2026_08", EstimatedRows: 30},
			{Schema: "public", Name: "rounds_2026_09", EstimatedRows: 42},
		}},
	}
	if got := GroupScans(scans, roots); !reflect.DeepEqual(got, want) {
		t.Errorf("GroupScans = %+v, want %+v", got, want)
	}
}

func TestAPlanReadingNoPartitionReportsNone(t *testing.T) {
	t.Parallel()
	got := GroupScans([]explainjson.Scan{{Schema: "public", Name: "member", Rows: 7}}, map[[2]string]PartitionRoot{})
	if len(got) != 0 {
		t.Errorf("GroupScans = %+v, want none", got)
	}
}
