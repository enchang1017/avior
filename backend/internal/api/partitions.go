package api

import (
	"context"
	"errors"
	"time"

	"github.com/enchang1017/avior/backend/internal/feature/query"
	"github.com/enchang1017/avior/backend/internal/feature/schema"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// PartitionScanner reads which partitions a statement's plan scans. FEAT-075.
type PartitionScanner interface {
	Scans(ctx context.Context, database, statement string) ([]query.PartitionScan, error)
}

// UsePartitionScans attaches FEAT-075's plan reading to inspect.
func (s *Server) UsePartitionScans(scanner PartitionScanner) { s.partitionScans = scanner }

// scansFor reads the partitions an inspected statement scans, and only for one
// the guard planned: a statement refused before its plan was made is never
// planned here either, whatever it says.
func (s *Server) scansFor(
	ctx context.Context, verdict sqlguard.Verdict, sub query.Submission,
) (*[]gen.PartitionScan, error) {
	if s.partitionScans == nil || verdict.EstimatedCost == nil {
		return nil, nil
	}
	scans, err := s.partitionScans.Scans(ctx, sub.Database, sub.Statement)
	if err != nil {
		return nil, err
	}
	if len(scans) == 0 {
		return nil, nil
	}
	out := make([]gen.PartitionScan, 0, len(scans))
	for _, scan := range scans {
		mapped := gen.PartitionScan{
			Schema: scan.Schema, Name: scan.Name, Total: scan.Total,
			Scanned: make([]gen.ScannedPartition, 0, len(scan.Scanned)),
		}
		for _, partition := range scan.Scanned {
			mapped.Scanned = append(mapped.Scanned, gen.ScannedPartition{
				Schema: partition.Schema, Name: partition.Name, EstimatedRows: partition.EstimatedRows,
			})
		}
		out = append(out, mapped)
	}
	return &out, nil
}

// GetTablePartitions is FEAT-075: how one table is partitioned.
func (s *Server) GetTablePartitions(
	ctx context.Context, request gen.GetTablePartitionsRequestObject,
) (gen.GetTablePartitionsResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetTablePartitions401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	params := request.Params
	partitioning, err := s.schema.Partitioning(ctx, schema.Caller{UserID: caller.ID, Roles: caller.Roles},
		params.Database, params.Schema, params.Table, time.Now())
	switch {
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.GetTablePartitions403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(params.Database)}, nil
	case errors.Is(err, schema.ErrDatabaseNotGoverned):
		return gen.GetTablePartitions404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
	case errors.Is(err, schema.ErrTableNotFound):
		return gen.GetTablePartitions404JSONResponse{NotFoundJSONResponse: tableUnknown(
			params.Database, params.Schema, params.Table)}, nil
	case err != nil:
		return nil, err
	}
	return gen.GetTablePartitions200JSONResponse(toPartitioning(partitioning)), nil
}

func toPartitioning(p schema.Partitioning) gen.TablePartitioning {
	out := gen.TablePartitioning{
		Schema:     p.Schema,
		Name:       p.Name,
		Partitions: make([]gen.PartitionEntry, 0, len(p.Partitions)),
		Warnings:   make([]gen.PartitionWarning, 0, len(p.Warnings)),
	}
	if p.Parent != nil {
		out.Parent = &gen.PartitionParent{Schema: p.Parent.Schema, Name: p.Parent.Name, Bound: p.Parent.Bound}
	}
	if p.Strategy != "" {
		strategy := gen.TablePartitioningStrategy(p.Strategy)
		key, total := p.Key, p.TotalBytes
		out.Strategy, out.Key, out.TotalBytes = &strategy, &key, &total
		out.ApproxRows = estimate(p.ApproxRows)
	}
	for _, partition := range p.Partitions {
		out.Partitions = append(out.Partitions, gen.PartitionEntry{
			Schema:      partition.Schema,
			Name:        partition.Name,
			Bound:       partition.Bound,
			IsDefault:   partition.Default,
			Partitioned: partition.Partitioned,
			ApproxRows:  estimate(partition.ApproxRows),
			TotalBytes:  partition.TotalBytes,
		})
	}
	for _, warning := range p.Warnings {
		mapped := gen.PartitionWarning{Code: gen.PartitionWarningCode(warning.Code), Message: warning.Message}
		if warning.Subject != "" {
			subject := warning.Subject
			mapped.Subject = &subject
		}
		out.Warnings = append(out.Warnings, mapped)
	}
	return out
}
