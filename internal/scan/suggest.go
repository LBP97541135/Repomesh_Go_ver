package scan

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// KeywordSuggester is the built-in 辅助推荐 engine: deterministic keyword
// matching between the requirement text and each card's searchable surface
// (name, description, topics, languages, deps, evidence, identities).
// Deliberately LLM-free — the suggestion is an evidence-counted hint, never
// a calibrated probability.
type KeywordSuggester struct {
	Store CatalogStore
}

var requirementTokens = regexp.MustCompile(`[\w-]+`)

// suggestStopwords: generic words that match everything and rank nothing.
var suggestStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "add": true,
	"support": true, "update": true, "fix": true, "refactor": true,
	"in": true, "to": true, "of": true, "a": true, "an": true,
}

// Suggest implements ScopeSuggester.
func (s KeywordSuggester) Suggest(requirement string, limit int) ([]Suggestion, error) {
	cards, err := s.Store.List(context.Background())
	if err != nil {
		return nil, err
	}
	tokens := requirementTokens.FindAllString(strings.ToLower(requirement), -1)
	kept := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if !suggestStopwords[token] {
			kept = append(kept, token)
		}
	}
	if len(kept) == 0 {
		return []Suggestion{}, nil
	}

	var suggestions []Suggestion
	for _, card := range cards {
		if card.AutoCard == nil {
			continue
		}
		matched := map[string]bool{}
		surface := strings.ToLower(strings.Join([]string{
			card.Name, card.Description,
			strings.Join(card.Topics, " "),
			strings.Join(card.AutoCard.Deps, " "),
			strings.Join(card.AutoCard.Identities, " "),
		}, " "))
		for _, token := range kept {
			if strings.Contains(surface, token) {
				matched[token] = true
			}
		}
		if len(matched) == 0 {
			continue
		}
		terms := suggestionKeys(matched)
		suggestions = append(suggestions, Suggestion{
			RepositoryID: card.ID,
			Name:         card.Name,
			Score:        float64(len(matched)) / float64(len(kept)),
			MatchedTerms: terms,
			Rationale:    "matched: " + strings.Join(terms, ", "),
		})
	}

	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		return suggestions[i].Name < suggestions[j].Name
	})
	if limit > 0 && len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions, nil
}

func suggestionKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
