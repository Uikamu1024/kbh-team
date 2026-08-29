package pipeline

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"backend/internal/domain"
)

// titleSimilarityThreshold is just below the 0.36 score of the representative
// Japanese paraphrase case. This admits substantial shared phrases while
// rejecting titles that only happen to share a short prefix or suffix.
//
// KNOWN LIMITATION: character bigram Jaccard similarity cannot reliably
// separate "same story, reworded headline" from "different story, similar
// generic phrasing" for short Japanese titles — verified experimentally:
// unrelated headlines sharing common news phrasing (e.g. "〜が続く、〜に追い風"
// or "〜で〜が見頃を迎える") can score higher than genuine paraphrases of the
// same story. Neither larger n-grams nor per-batch IDF weighting fixed this
// in testing. A proper fix needs semantic similarity (e.g. Gemini embeddings
// + cosine similarity) instead of character overlap; left as a Phase 4
// precision refinement (see backend/CLAUDE.md開発フェーズ) rather than blocking
// Phase 1, since docs/pipeline/02-dedupe.md explicitly allows either approach
// and no embedding call exists yet in this codebase to validate against.
const titleSimilarityThreshold = 0.35

type topicGroup struct {
	articles []domain.Article
}

// DedupeArticles groups articles with sufficiently similar titles into topics.
// The first article remains the group's order anchor; the longest article body
// is selected as Primary after all related articles have been collected.
func DedupeArticles(articles []domain.Article) []domain.Topic {
	if len(articles) == 0 {
		return nil
	}

	groups := make([]topicGroup, 0, len(articles))
	for _, article := range articles {
		groupIndex, found := bestMatchingGroup(article, groups)
		if !found {
			groups = append(groups, topicGroup{articles: []domain.Article{article}})
			continue
		}
		groups[groupIndex].articles = append(groups[groupIndex].articles, article)
	}

	topics := make([]domain.Topic, 0, len(groups))
	for _, group := range groups {
		primary := group.articles[0]
		for _, article := range group.articles[1:] {
			if bodyLength(article.Body) > bodyLength(primary.Body) {
				primary = article
			}
		}
		topics = append(topics, domain.Topic{
			Primary:      primary,
			RelatedCount: len(group.articles),
		})
	}
	return topics
}

func bestMatchingGroup(article domain.Article, groups []topicGroup) (int, bool) {
	bestGroup := -1
	bestSimilarity := 0.0
	for groupIndex, group := range groups {
		for _, related := range group.articles {
			similarity := titleSimilarity(article.Title, related.Title)
			if similarity >= titleSimilarityThreshold && similarity > bestSimilarity {
				bestGroup = groupIndex
				bestSimilarity = similarity
			}
		}
	}
	return bestGroup, bestGroup >= 0
}

func titleSimilarity(left, right string) float64 {
	left = normalizeTitle(left)
	right = normalizeTitle(right)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 1
	}

	leftBigrams := titleBigrams(left)
	rightBigrams := titleBigrams(right)
	if len(leftBigrams) == 0 || len(rightBigrams) == 0 {
		return 0
	}

	intersection := 0
	for bigram := range leftBigrams {
		if _, ok := rightBigrams[bigram]; ok {
			intersection++
		}
	}
	union := len(leftBigrams) + len(rightBigrams) - intersection
	return float64(intersection) / float64(union)
}

// TitleSimilarity returns the character-bigram Jaccard similarity used by
// DedupeArticles. The ingestion job uses the same threshold and algorithm
// when comparing a new article with the cached article groups.
func TitleSimilarity(left, right string) float64 {
	return titleSimilarity(left, right)
}

// TitleSimilarityThreshold is the minimum similarity at which two article
// titles are considered to cover the same topic.
const TitleSimilarityThreshold = titleSimilarityThreshold

func normalizeTitle(title string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

func titleBigrams(title string) map[string]struct{} {
	runes := []rune(title)
	capacity := len(runes) - 1
	if capacity < 0 {
		capacity = 0
	}
	bigrams := make(map[string]struct{}, capacity)
	for index := 0; index+1 < len(runes); index++ {
		bigrams[string(runes[index:index+2])] = struct{}{}
	}
	return bigrams
}

func bodyLength(body string) int {
	return utf8.RuneCountInString(body)
}
