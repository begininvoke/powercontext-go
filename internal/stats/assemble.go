package stats

import (
	"fmt"
	"slices"
	"time"

	"github.com/ob-labs/powercontext-go/inference"
)

// InventoryCounts is a persistence-neutral snapshot of current relational
// heads. MemoryEntries includes authoritative manifest state, not search-index
// state.
type InventoryCounts struct {
	Sources       int64
	Artifacts     []ArtifactCountRow
	Candidates    []CandidateCountRow
	MemoryEntries []MemoryEntryStateRow
}

type ArtifactCountRow struct {
	Family string
	Total  int64
}

type CandidateCountRow struct {
	Family string
	Status string
	Total  int64
}

type MemoryEntryStateRow struct {
	Kind  string
	State string
}

type StoredModelUsage struct {
	UsageDate                           time.Time
	Purpose                             ModelPurpose
	Operation                           ModelOperation
	Requests, InputTokens, OutputTokens int64
	InputComplete, OutputComplete       bool
}

type StoredRecallTokenUsage struct {
	UsageDate                                               time.Time
	Preparations, ReadyPreparations, ComparablePreparations int64
	BaselineTokens, RecalledTokens                          int64
}

func Build(
	scopeID string,
	asOf time.Time,
	preset Period,
	processed int64,
	inventory InventoryCounts,
	usageRows []StoredModelUsage,
	estimator *inference.TokenEstimatorProfile,
	recallRows []StoredRecallTokenUsage,
) (Statistics, error) {
	if scopeID == "" || asOf.IsZero() {
		return Statistics{}, fmt.Errorf("statistics scope and as_of must be configured")
	}
	if processed < 0 || inventory.Sources < 0 {
		return Statistics{}, fmt.Errorf("statistics Source positions must be non-negative")
	}
	asOf = asOf.UTC()
	period, err := ResolvePeriod(preset, asOf)
	if err != nil {
		return Statistics{}, err
	}
	assembledInventory, err := assembleInventory(processed, inventory)
	if err != nil {
		return Statistics{}, err
	}
	usage, err := assembleUsage(period, usageRows)
	if err != nil {
		return Statistics{}, err
	}
	recall, err := assembleRecall(period, estimator, recallRows)
	if err != nil {
		return Statistics{}, err
	}
	return Statistics{
		scopeID: scopeID, asOf: asOf, inventory: assembledInventory,
		usage: usage, recall: recall,
	}, nil
}

func assembleInventory(processed int64, counts InventoryCounts) (Inventory, error) {
	artifacts := make([]FamilyCount, len(counts.Artifacts))
	artifactTotal := int64(0)
	for index, row := range counts.Artifacts {
		value, err := NewFamilyCount(row.Family, row.Total)
		if err != nil {
			return Inventory{}, err
		}
		artifacts[index], artifactTotal = value, artifactTotal+row.Total
	}
	slices.SortFunc(artifacts, func(left, right FamilyCount) int { return stringCompare(left.family, right.family) })

	candidateCounts := make(map[string]map[string]int64)
	for _, row := range counts.Candidates {
		if row.Family == "" || row.Total < 0 || (row.Status != "pending" && row.Status != "approved" && row.Status != "rejected") {
			return Inventory{}, fmt.Errorf("invalid Candidate inventory row")
		}
		statuses := candidateCounts[row.Family]
		if statuses == nil {
			statuses = map[string]int64{"pending": 0, "approved": 0, "rejected": 0}
			candidateCounts[row.Family] = statuses
		}
		statuses[row.Status] += row.Total
	}
	candidates := make([]CandidateFamilyCount, 0, len(candidateCounts))
	for family, statuses := range candidateCounts {
		candidates = append(candidates, newCandidateFamilyCount(
			family, statuses["pending"], statuses["approved"], statuses["rejected"],
		))
	}
	slices.SortFunc(candidates, func(left, right CandidateFamilyCount) int { return stringCompare(left.family, right.family) })
	candidateInventory := CandidateInventory{byFamily: candidates}
	for _, value := range candidates {
		candidateInventory.total += value.total
		candidateInventory.pending += value.pending
		candidateInventory.approved += value.approved
		candidateInventory.rejected += value.rejected
	}

	memoryCounts := make(map[string]map[string]int64)
	for _, row := range counts.MemoryEntries {
		if row.Kind == "" || (row.State != "active" && row.State != "inactive") {
			return Inventory{}, fmt.Errorf("invalid Memory inventory row")
		}
		states := memoryCounts[row.Kind]
		if states == nil {
			states = map[string]int64{"active": 0, "inactive": 0}
			memoryCounts[row.Kind] = states
		}
		states[row.State]++
	}
	memoryKinds := make([]MemoryKindCount, 0, len(memoryCounts))
	for kind, states := range memoryCounts {
		memoryKinds = append(memoryKinds, newMemoryKindCount(kind, states["active"], states["inactive"]))
	}
	slices.SortFunc(memoryKinds, func(left, right MemoryKindCount) int { return stringCompare(left.kind, right.kind) })
	memoryInventory := MemoryEntryInventory{byKind: memoryKinds}
	for _, value := range memoryKinds {
		memoryInventory.total += value.total
		memoryInventory.active += value.active
		memoryInventory.inactive += value.inactive
	}

	pending := counts.Sources - processed
	if pending < 0 {
		pending = 0
	}
	return Inventory{
		sources:    SourceInventory{total: counts.Sources, memoryProcessed: processed, memoryPending: pending},
		artifacts:  ArtifactInventory{total: artifactTotal, byFamily: artifacts},
		candidates: candidateInventory,
		memory:     memoryInventory,
	}, nil
}

func assembleUsage(period ResolvedPeriod, rows []StoredModelUsage) (Usage, error) {
	for _, row := range rows {
		if err := validateStoredUsage(row); err != nil {
			return Usage{}, err
		}
	}
	daily := make([]ModelUsageDay, 0, dateCount(period.startDate, period.endDate))
	for day := period.startDate; !day.After(period.endDate); day = day.AddDate(0, 0, 1) {
		selected := usageForDate(rows, day)
		daily = append(daily, ModelUsageDay{
			date: day,
			usage: ModelUsage{
				generation: operationTotal(selected, Generation),
				embedding:  operationTotal(selected, Embedding),
			},
			byPurpose: purposeBreakdown(selected),
		})
	}
	return Usage{
		period:    period,
		totals:    ModelUsage{generation: operationTotal(rows, Generation), embedding: operationTotal(rows, Embedding)},
		byPurpose: purposeBreakdown(rows), daily: daily,
	}, nil
}

func assembleRecall(
	period ResolvedPeriod,
	estimator *inference.TokenEstimatorProfile,
	rows []StoredRecallTokenUsage,
) (Recall, error) {
	byDate := make(map[string]StoredRecallTokenUsage, len(rows))
	for _, row := range rows {
		if err := validateStoredRecall(row); err != nil {
			return Recall{}, err
		}
		key := utcDate(row.UsageDate).Format(time.DateOnly)
		if _, duplicate := byDate[key]; duplicate {
			return Recall{}, fmt.Errorf("duplicate recall usage date %s", key)
		}
		byDate[key] = row
	}
	daily := make([]RecallTokenDay, 0, dateCount(period.startDate, period.endDate))
	totals := RecallTokenValue{}
	for day := period.startDate; !day.After(period.endDate); day = day.AddDate(0, 0, 1) {
		row := byDate[day.Format(time.DateOnly)]
		value := RecallTokenValue{
			preparations: row.Preparations, readyPreparations: row.ReadyPreparations,
			comparablePreparations: row.ComparablePreparations,
			baselineTokens:         row.BaselineTokens, recalledTokens: row.RecalledTokens,
			tokenReduction: row.BaselineTokens - row.RecalledTokens,
		}
		daily = append(daily, RecallTokenDay{date: day, RecallTokenValue: value})
		totals.preparations += value.preparations
		totals.readyPreparations += value.readyPreparations
		totals.comparablePreparations += value.comparablePreparations
		totals.baselineTokens += value.baselineTokens
		totals.recalledTokens += value.recalledTokens
	}
	totals.tokenReduction = totals.baselineTokens - totals.recalledTokens
	var profile *inference.TokenEstimatorProfile
	if estimator != nil {
		copy := *estimator
		profile = &copy
	}
	return Recall{period: period, estimator: profile, totals: totals, daily: daily}, nil
}

func validateStoredUsage(row StoredModelUsage) error {
	if row.UsageDate.IsZero() || row.Requests < 0 || row.InputTokens < 0 || row.OutputTokens < 0 {
		return fmt.Errorf("invalid stored model usage")
	}
	if err := row.Purpose.Validate(); err != nil {
		return err
	}
	return row.Operation.Validate()
}

func validateStoredRecall(row StoredRecallTokenUsage) error {
	if row.UsageDate.IsZero() || row.Preparations < 0 || row.ReadyPreparations < 0 ||
		row.ComparablePreparations < 0 || row.BaselineTokens < 0 || row.RecalledTokens < 0 ||
		row.ComparablePreparations > row.ReadyPreparations || row.ReadyPreparations > row.Preparations {
		return fmt.Errorf("invalid stored recall token usage")
	}
	return nil
}

func usageForDate(rows []StoredModelUsage, date time.Time) []StoredModelUsage {
	result := make([]StoredModelUsage, 0)
	for _, row := range rows {
		if utcDate(row.UsageDate).Equal(date) {
			result = append(result, row)
		}
	}
	return result
}

func operationTotal(rows []StoredModelUsage, operation ModelOperation) ModelUsageValue {
	selected := make([]ModelUsageValue, 0)
	for _, row := range rows {
		if row.Operation != operation {
			continue
		}
		var input, output *int64
		if row.InputComplete {
			value := row.InputTokens
			input = &value
		}
		if row.OutputComplete {
			value := row.OutputTokens
			output = &value
		}
		selected = append(selected, ModelUsageValue{requests: row.Requests, inputTokens: input, outputTokens: output})
	}
	return usageTotal(selected)
}

func usageTotal(values []ModelUsageValue) ModelUsageValue {
	result := ModelUsageValue{}
	inputComplete, outputComplete := true, true
	for _, value := range values {
		result.requests += value.requests
		if value.requests <= 0 {
			continue
		}
		if value.inputTokens == nil {
			inputComplete = false
		} else if inputComplete {
			resultValue := int64Value(result.inputTokens) + *value.inputTokens
			result.inputTokens = &resultValue
		}
		if value.outputTokens == nil {
			outputComplete = false
		} else if outputComplete {
			resultValue := int64Value(result.outputTokens) + *value.outputTokens
			result.outputTokens = &resultValue
		}
	}
	if !inputComplete {
		result.inputTokens = nil
	} else if result.inputTokens == nil {
		zero := int64(0)
		result.inputTokens = &zero
	}
	if !outputComplete {
		result.outputTokens = nil
	} else if result.outputTokens == nil {
		zero := int64(0)
		result.outputTokens = &zero
	}
	return result
}

func purposeBreakdown(rows []StoredModelUsage) []PurposeBreakdown {
	seen := make(map[ModelPurpose]struct{})
	for _, row := range rows {
		seen[row.Purpose] = struct{}{}
	}
	result := make([]PurposeBreakdown, 0, len(seen))
	for _, purpose := range modelPurposes {
		if _, exists := seen[purpose]; !exists {
			continue
		}
		selected := make([]StoredModelUsage, 0)
		for _, row := range rows {
			if row.Purpose == purpose {
				selected = append(selected, row)
			}
		}
		result = append(result, PurposeBreakdown{
			purpose:    purpose,
			generation: operationTotal(selected, Generation),
			embedding:  operationTotal(selected, Embedding),
		})
	}
	return result
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
func dateCount(start, end time.Time) int { return int(end.Sub(start).Hours()/24) + 1 }
func stringCompare(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
