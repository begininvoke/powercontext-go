package memory

import (
	"context"
	"reflect"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
)

func (s *Service) setEntryState(
	ctx context.Context,
	value Memory,
	entries []EntryVersion,
	target EntryState,
	reason *string,
) (Memory, error) {
	base, err := s.canonicalBase(ctx, &value)
	if err != nil {
		return Memory{}, err
	}
	currentEntries, err := s.validatedEntries(ctx, *base)
	if err != nil {
		return Memory{}, err
	}
	manifest := manifestMap(base.Content().Manifest().Entries())
	current := entryMap(currentEntries)
	normalizedReason, err := NormalizeReason(reason)
	if err != nil {
		return Memory{}, err
	}
	changes := make([]Change, 0)
	seen := make(map[string]struct{})
	for _, requested := range entries {
		identity := requested.EntryID + "\x00" + requested.EntryVersionID
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		entryID, err := ValidateIdentifier(requested.EntryID)
		if err != nil {
			return Memory{}, err
		}
		item, exists := manifest[entryID]
		if !exists {
			return Memory{}, &EntryNotFoundError{EntryID: entryID}
		}
		currentEntry, exists := current[entryID]
		if !exists {
			return Memory{}, &EntryNotFoundError{EntryID: entryID}
		}
		if !reflect.DeepEqual(requested.Clone(), currentEntry) {
			return Memory{}, &InvalidCitationError{Code: "entry-mismatch"}
		}
		if item.State() == target {
			continue
		}
		updated, _ := NewManifestEntry(item.EntryID(), item.EntryVersionID(), item.EntryContentHash(), target)
		manifest[entryID] = updated
		var op ChangeOp
		var from, to *string
		if target == Active {
			op = Reactivate
			to = ptrString(item.EntryVersionID())
		} else {
			op = Deactivate
			from = ptrString(item.EntryVersionID())
		}
		change, err := NewChange(op, entryID, from, to, normalizedReason)
		if err != nil {
			return Memory{}, err
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return *base, nil
	}
	return s.commitExistingTransition(ctx, *base, manifest, changes, current, nil)
}

func (s *Service) commitExistingTransition(
	ctx context.Context,
	base Memory,
	manifest map[string]ManifestEntry,
	changes []Change,
	current map[string]EntryVersion,
	newVersions []EntryVersion,
) (Memory, error) {
	manifestEntries := sortedManifest(manifest)
	changes = sortedChanges(changes)
	content := NewContent(NewManifest(manifestEntries), changes)
	draft, err := NewDraft(content, nil, nil)
	if err != nil {
		return Memory{}, err
	}
	next, err := artifact.New(base.ID(), base.Revision()+1, draft)
	if err != nil {
		return Memory{}, err
	}
	changedIDs := make(map[string]struct{}, len(newVersions))
	for _, version := range newVersions {
		changedIDs[version.EntryVersionID] = struct{}{}
	}
	projections, err := s.prepareProjections(ctx, &base, manifestEntries, current, changedIDs)
	if err != nil {
		return Memory{}, err
	}
	hash, err := ContentHash(content)
	if err != nil {
		return Memory{}, err
	}
	return s.backend.Commit(ctx, NewCommit(&base, next, hash, newVersions, projections))
}

func (s *Service) deduplicateManifest(
	manifest map[string]ManifestEntry,
	current map[string]EntryVersion,
) ([]Change, map[string]struct{}, error) {
	groups := make(map[string][]string)
	for _, item := range manifest {
		if item.State() != Active {
			continue
		}
		version, exists := current[item.EntryID()]
		if !exists {
			return nil, nil, &InvalidCitationError{Code: "missing-version"}
		}
		material, err := s.materialFromVersion(version)
		if err != nil {
			return nil, nil, err
		}
		groups[string(material.contentBytes)] = append(groups[string(material.contentBytes)], item.EntryID())
	}
	changes := make([]Change, 0)
	changed := make(map[string]struct{})
	for _, ids := range groups {
		slices.Sort(ids)
		for _, entryID := range ids[1:] {
			item := manifest[entryID]
			updated, _ := NewManifestEntry(item.EntryID(), item.EntryVersionID(), item.EntryContentHash(), Inactive)
			manifest[entryID] = updated
			reason := "dedupe"
			from := item.EntryVersionID()
			change, _ := NewChange(Deactivate, entryID, &from, nil, &reason)
			changes = append(changes, change)
			changed[entryID] = struct{}{}
		}
	}
	return changes, changed, nil
}

func (s *Service) normalizeManifestEntries(
	base Memory,
	manifest map[string]ManifestEntry,
	current map[string]EntryVersion,
	skip map[string]struct{},
) ([]Change, []EntryVersion, error) {
	changes := make([]Change, 0)
	versions := make([]EntryVersion, 0)
	entryIDs := make([]string, 0, len(current))
	for entryID := range current {
		entryIDs = append(entryIDs, entryID)
	}
	slices.Sort(entryIDs)
	for _, entryID := range entryIDs {
		if _, skipped := skip[entryID]; skipped {
			continue
		}
		previous := current[entryID]
		material, err := s.materialFromVersion(previous)
		if err != nil {
			return nil, nil, err
		}
		if isCanonicalVersion(previous, material) {
			continue
		}
		version, err := s.newEntryVersion(base.ID(), entryID, &previous, material, base.Revision()+1)
		if err != nil {
			return nil, nil, err
		}
		item := manifest[entryID]
		manifest[entryID], _ = NewManifestEntry(entryID, version.EntryVersionID, version.EntryContentHash, item.State())
		current[entryID] = version
		versions = append(versions, version)
		reason := "normalize"
		change, _ := NewChange(Revise, entryID, &previous.EntryVersionID, &version.EntryVersionID, &reason)
		changes = append(changes, change)
	}
	return changes, versions, nil
}
