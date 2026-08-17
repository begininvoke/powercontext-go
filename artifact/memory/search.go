package memory

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	rrfConstant           = 60
	minSemanticSimilarity = 0.3
)

var fold = cases.Fold()

func AnalyzeText(value string) string {
	value = fold.String(norm.NFC.String(value))
	terms := make([]string, 0)
	word := make([]rune, 0)
	cjk := make([]rune, 0)
	flushWord := func() {
		if len(word) > 0 {
			terms = append(terms, string(word))
			word = word[:0]
		}
	}
	flushCJK := func() {
		if len(cjk) == 0 {
			return
		}
		for _, character := range cjk {
			terms = append(terms, fmt.Sprintf("u_%x", character))
		}
		for index := 1; index < len(cjk); index++ {
			terms = append(terms, fmt.Sprintf("b_%x_%x", cjk[index-1], cjk[index]))
		}
		cjk = cjk[:0]
	}
	for _, character := range value {
		switch {
		case isCJK(character):
			flushWord()
			cjk = append(cjk, character)
		case unicode.IsLetter(character) || unicode.IsNumber(character) || character == '_':
			flushCJK()
			word = append(word, character)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return strings.Join(terms, " ")
}

func FTSMatchQuery(value string) (string, bool) {
	analyzed := AnalyzeText(value)
	if analyzed == "" {
		return "", false
	}
	terms := strings.Fields(analyzed)
	for index := range terms {
		terms[index] = `"` + strings.ReplaceAll(terms[index], `"`, `""`) + `"`
	}
	return strings.Join(terms, " OR "), true
}

func AdmitsFTSText(query, text string) bool {
	queryTerms := termSet(AnalyzeText(query))
	if len(queryTerms) == 0 {
		return false
	}
	required := 1
	if len(queryTerms) > 2 {
		required = max(2, int(math.Ceil(float64(len(queryTerms))*0.25)))
	}
	textTerms := termSet(AnalyzeText(text))
	matches := 0
	for term := range queryTerms {
		if _, exists := textTerms[term]; exists {
			matches++
		}
	}
	return matches >= required
}

func AdmitFTSCandidates(query string, candidates []ChannelHit) []ChannelHit {
	result := make([]ChannelHit, 0, len(candidates))
	for _, candidate := range candidates {
		if AdmitsFTSText(query, candidate.Text) {
			result = append(result, candidate)
		}
	}
	return result
}

func AdmitVectorCandidates(candidates []ChannelHit) []ChannelHit {
	result := make([]ChannelHit, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Distance != nil && unitL2CosineSimilarity(*candidate.Distance) >= minSemanticSimilarity {
			result = append(result, candidate)
		}
	}
	return result
}

type hitIdentity struct {
	artifactID     string
	revision       int64
	entryID        string
	entryVersionID string
}

func FuseRankings(fts, vector []ChannelHit, limit int) ([]Hit, error) {
	if limit < 1 {
		return nil, fmt.Errorf("memory search limit must be positive")
	}
	candidates := make(map[hitIdentity]ChannelHit)
	scores := make(map[hitIdentity]float64)
	channels := make(map[hitIdentity]map[MatchedBy]struct{})
	for _, ranking := range []struct {
		channel MatchedBy
		hits    []ChannelHit
	}{{MatchedFTS, fts}, {MatchedVector, vector}} {
		seen := make(map[hitIdentity]struct{})
		for index, candidate := range ranking.hits {
			identity := identity(candidate)
			if _, exists := seen[identity]; exists {
				continue
			}
			seen[identity] = struct{}{}
			if _, exists := candidates[identity]; !exists {
				candidates[identity] = candidate
			}
			scores[identity] += 1.0 / float64(rrfConstant+index+1)
			if channels[identity] == nil {
				channels[identity] = make(map[MatchedBy]struct{})
			}
			channels[identity][ranking.channel] = struct{}{}
		}
	}
	identities := make([]hitIdentity, 0, len(candidates))
	for value := range candidates {
		identities = append(identities, value)
	}
	slices.SortFunc(identities, func(left, right hitIdentity) int {
		if scores[left] > scores[right] {
			return -1
		}
		if scores[left] < scores[right] {
			return 1
		}
		if order := strings.Compare(left.artifactID, right.artifactID); order != 0 {
			return order
		}
		if order := strings.Compare(left.entryID, right.entryID); order != 0 {
			return order
		}
		return strings.Compare(left.entryVersionID, right.entryVersionID)
	})
	if len(identities) > limit {
		identities = identities[:limit]
	}
	result := make([]Hit, 0, len(identities))
	for _, identity := range identities {
		candidate := candidates[identity]
		matched := make([]MatchedBy, 0, 2)
		for _, channel := range []MatchedBy{MatchedFTS, MatchedVector} {
			if _, exists := channels[identity][channel]; exists {
				matched = append(matched, channel)
			}
		}
		result = append(result, Hit{
			MemoryRef: candidate.MemoryRef, EntryID: candidate.EntryID,
			EntryVersionID: candidate.EntryVersionID, Text: candidate.Text,
			Score: scores[identity], MatchedBy: matched,
		})
	}
	return result, nil
}

func identity(hit ChannelHit) hitIdentity {
	return hitIdentity{
		artifactID: hit.MemoryRef.ID(), revision: hit.MemoryRef.Revision(),
		entryID: hit.EntryID, entryVersionID: hit.EntryVersionID,
	}
}

func unitL2CosineSimilarity(distance float64) float64 {
	return max(-1, min(1, 1-distance*distance/2))
}

func termSet(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, term := range strings.Fields(value) {
		result[term] = struct{}{}
	}
	return result
}

func isCJK(character rune) bool {
	return character >= 0x3400 && character <= 0x4DBF ||
		character >= 0x4E00 && character <= 0x9FFF ||
		character >= 0xF900 && character <= 0xFAFF ||
		character >= 0x20000 && character <= 0x2FA1F
}
