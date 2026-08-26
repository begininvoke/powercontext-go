package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/stats"
)

type StatisticsOperations interface {
	Overview(context.Context, string, stats.Period) (stats.Statistics, error)
}

func (h *Handler) GetStats(ctx context.Context, params v1.GetStatsParams) (v1.GetStatsRes, error) {
	if h.statistics == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.statistics.Overview(ctx, params.ScopeID, stats.Period(params.Period.Or(v1.StatsPeriod30d)))
	if err != nil {
		return nil, err
	}
	return &v1.ScopedStatsHeaders{
		CacheControl:           v1.NewOptGetStatsOKCacheControl(v1.GetStatsOKCacheControlNoStore),
		XPowerContextRequestID: requestID(ctx),
		Response:               scopedStatistics(value),
	}, nil
}

func scopedStatistics(value stats.Statistics) v1.ScopedStats {
	inventory := value.Inventory()
	usage := value.Usage()
	recall := value.Recall()
	return v1.ScopedStats{
		ScopeID: value.ScopeID(), AsOf: value.AsOf(),
		Inventory: inventoryStatistics(inventory),
		Usage:     usageStatistics(usage),
		Recall:    recallStatistics(recall),
	}
}

func inventoryStatistics(value stats.Inventory) v1.InventoryStatistics {
	sources := value.Sources()
	artifacts := value.Artifacts()
	artifactFamilies := make([]v1.FamilyCount, 0, len(artifacts.ByFamily()))
	for _, item := range artifacts.ByFamily() {
		artifactFamilies = append(artifactFamilies, v1.FamilyCount{Family: item.Family(), Total: int(item.Total())})
	}
	candidates := value.Candidates()
	candidateFamilies := make([]v1.CandidateFamilyCount, 0, len(candidates.ByFamily()))
	for _, item := range candidates.ByFamily() {
		candidateFamilies = append(candidateFamilies, v1.CandidateFamilyCount{
			Family: v1.CandidateFamily(item.Family()), Total: int(item.Total()),
			Pending: int(item.Pending()), Approved: int(item.Approved()), Rejected: int(item.Rejected()),
		})
	}
	entries := value.MemoryEntries()
	memoryKinds := make([]v1.MemoryKindCount, 0, len(entries.ByKind()))
	for _, item := range entries.ByKind() {
		memoryKinds = append(memoryKinds, v1.MemoryKindCount{
			Kind: item.Kind(), Total: int(item.Total()), Active: int(item.Active()), Inactive: int(item.Inactive()),
		})
	}
	return v1.InventoryStatistics{
		Sources: v1.SourceInventoryStatistics{
			Total: int(sources.Total()), MemoryProcessed: int(sources.MemoryProcessed()),
			MemoryPending: int(sources.MemoryPending()),
		},
		Artifacts: v1.ArtifactInventoryStatistics{Total: int(artifacts.Total()), ByFamily: artifactFamilies},
		Candidates: v1.CandidateInventoryStatistics{
			Total: int(candidates.Total()), Pending: int(candidates.Pending()),
			Approved: int(candidates.Approved()), Rejected: int(candidates.Rejected()), ByFamily: candidateFamilies,
		},
		Memory: v1.MemoryInventoryStatistics{Entries: v1.MemoryEntryInventoryStatistics{
			Total: int(entries.Total()), Active: int(entries.Active()), Inactive: int(entries.Inactive()), ByKind: memoryKinds,
		}},
	}
}

func usageStatistics(value stats.Usage) v1.UsageStatistics {
	daily := make([]v1.ModelUsageDay, 0, len(value.Daily()))
	for _, day := range value.Daily() {
		usage := day.Usage()
		daily = append(daily, v1.ModelUsageDay{
			Date: day.Date(), Generation: modelUsageValue(usage.Generation()),
			Embedding: modelUsageValue(usage.Embedding()), ByPurpose: purposeBreakdowns(day.ByPurpose()),
		})
	}
	return v1.UsageStatistics{
		Period: resolvedPeriod(value.Period()), Totals: modelUsage(value.Totals()),
		ByPurpose: purposeBreakdowns(value.ByPurpose()), Daily: daily,
	}
}

func modelUsage(value stats.ModelUsage) v1.ModelUsageStatistics {
	return v1.ModelUsageStatistics{
		Generation: modelUsageValue(value.Generation()), Embedding: modelUsageValue(value.Embedding()),
	}
}

func modelUsageValue(value stats.ModelUsageValue) v1.ModelUsageValue {
	return v1.ModelUsageValue{
		Requests: int(value.Requests()), InputTokens: nullableInt64(value.InputTokens()),
		OutputTokens: nullableInt64(value.OutputTokens()),
	}
}

func purposeBreakdowns(values []stats.PurposeBreakdown) []v1.ModelUsagePurposeBreakdown {
	result := make([]v1.ModelUsagePurposeBreakdown, len(values))
	for index, value := range values {
		result[index] = v1.ModelUsagePurposeBreakdown{
			Purpose: string(value.Purpose()), Generation: modelUsageValue(value.Generation()),
			Embedding: modelUsageValue(value.Embedding()),
		}
	}
	return result
}

func recallStatistics(value stats.Recall) v1.RecallTokenStatistics {
	estimator := v1.NilTokenEstimatorProfile{}
	if profile := value.Estimator(); profile != nil {
		estimator.SetTo(v1.TokenEstimatorProfile{EstimatorID: profile.EstimatorID(), Version: profile.Version()})
	} else {
		estimator.SetToNull()
	}
	daily := make([]v1.RecallTokenDay, 0, len(value.Daily()))
	for _, day := range value.Daily() {
		wire := recallTokenValue(day.RecallTokenValue)
		daily = append(daily, v1.RecallTokenDay{
			Date: day.Date(), Preparations: wire.Preparations, ReadyPreparations: wire.ReadyPreparations,
			ComparablePreparations: wire.ComparablePreparations, BaselineTokens: wire.BaselineTokens,
			RecalledTokens: wire.RecalledTokens, TokenReduction: wire.TokenReduction,
		})
	}
	return v1.RecallTokenStatistics{
		Period: resolvedPeriod(value.Period()), Estimator: estimator,
		Totals: recallTokenValue(value.Totals()), Daily: daily,
	}
}

func recallTokenValue(value stats.RecallTokenValue) v1.RecallTokenValue {
	return v1.RecallTokenValue{
		Preparations: int(value.Preparations()), ReadyPreparations: int(value.ReadyPreparations()),
		ComparablePreparations: int(value.ComparablePreparations()), BaselineTokens: int(value.BaselineTokens()),
		RecalledTokens: int(value.RecalledTokens()), TokenReduction: int(value.TokenReduction()),
	}
}

func resolvedPeriod(value stats.ResolvedPeriod) v1.ResolvedUsagePeriod {
	return v1.ResolvedUsagePeriod{
		Preset: v1.StatsPeriod(value.Preset()), StartDate: value.StartDate(), EndDate: value.EndDate(),
		Timezone: v1.ResolvedUsagePeriodTimezoneUTC,
	}
}

func nullableInt64(value *int64) v1.NilInt {
	result := v1.NilInt{}
	if value == nil {
		result.SetToNull()
		return result
	}
	result.SetTo(int(*value))
	return result
}
