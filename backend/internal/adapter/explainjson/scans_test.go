package explainjson

import (
	"errors"
	"reflect"
	"testing"
)

// An Append over a partitioned table names each partition it reads, with the
// partition's own estimate rather than the Append's total.
func TestScansListEachPartitionAnAppendReads(t *testing.T) {
	t.Parallel()
	got, err := Scans(load(t, "partition-append.json"))
	if err != nil {
		t.Fatalf("Scans error = %v", err)
	}
	want := []Scan{
		{Schema: "public", Name: "rounds_2026_08", Rows: 970},
		{Schema: "public", Name: "rounds_2026_09", Rows: 970},
		{Schema: "public", Name: "rounds_default", Rows: 970},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scans = %+v, want %+v", got, want)
	}
}

// Pruned to one partition, PostgreSQL drops the Append and scans the partition
// directly. It is still a scan of that partition.
func TestScansReadAPartitionPrunedToOneWithoutAnAppend(t *testing.T) {
	t.Parallel()
	got, err := Scans(load(t, "partition-pruned.json"))
	if err != nil {
		t.Fatalf("Scans error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "events_2026_09" || got[0].Schema != "public" || got[0].Rows <= 0 {
		t.Errorf("Scans = %+v, want the one events_2026_09 scan with its estimate", got)
	}
}

// An ordered Append reads its partitions through their indexes. The index scan
// names the relation, and that is the partition.
func TestScansReadPartitionsThroughTheirIndexes(t *testing.T) {
	t.Parallel()
	got, err := Scans(load(t, "partition-ordered-append.json"))
	if err != nil {
		t.Fatalf("Scans error = %v", err)
	}
	names := make([]string, 0, len(got))
	for _, scan := range got {
		names = append(names, scan.Name)
	}
	if !reflect.DeepEqual(names, []string{"events_2026_09", "events_2026_10"}) {
		t.Errorf("Scans read %v, want both events partitions in plan order", names)
	}
}

func TestScansRefuseADocumentWithNoPlan(t *testing.T) {
	t.Parallel()
	if _, err := Scans([]byte(`[]`)); !errors.Is(err, ErrNoPlanInDocument) {
		t.Errorf("Scans([]) error = %v, want ErrNoPlanInDocument", err)
	}
	if _, err := Scans([]byte(`[{"NoPlan": {}}]`)); !errors.Is(err, ErrNoPlanInDocument) {
		t.Errorf("Scans without a Plan error = %v, want ErrNoPlanInDocument", err)
	}
	if _, err := Scans([]byte(`{`)); err == nil {
		t.Error("Scans accepted a malformed document")
	}
}
