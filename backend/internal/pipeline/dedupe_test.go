package pipeline

import (
	"testing"

	"backend/internal/domain"
)

func TestDedupeArticlesMergesJapaneseParaphrase(t *testing.T) {
	articles := []domain.Article{
		{
			Title: "AI新モデル発表、性能が前世代比2倍に",
			Body:  "短い本文",
		},
		{
			Title: "AI新モデルが発表される、前世代の2倍の性能",
			Body:  "こちらの方が本文が長く、代表記事に選ばれる本文です。",
		},
	}

	topics := DedupeArticles(articles)
	if len(topics) != 1 {
		t.Fatalf("expected one topic, got %d", len(topics))
	}
	if topics[0].RelatedCount != 2 {
		t.Fatalf("expected RelatedCount 2, got %d", topics[0].RelatedCount)
	}
	if topics[0].Primary.Title != articles[1].Title {
		t.Fatalf("expected longest-body article as Primary, got %q", topics[0].Primary.Title)
	}
}

func TestDedupeArticlesKeepsUnrelatedSimilarShapeSeparate(t *testing.T) {
	articles := []domain.Article{
		{Title: "AI新モデル発表", Body: "記事A"},
		{Title: "AI旧制度発表", Body: "記事B"},
	}

	topics := DedupeArticles(articles)
	if len(topics) != 2 {
		t.Fatalf("expected two unrelated topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if topic.RelatedCount != 1 {
			t.Fatalf("expected RelatedCount 1, got %d", topic.RelatedCount)
		}
	}
}

func TestTitleSimilarityUsesCharacterBigrams(t *testing.T) {
	if similarity := titleSimilarity("AI新モデル発表、性能が前世代比2倍に", "AI新モデルが発表される、前世代の2倍の性能"); similarity < titleSimilarityThreshold {
		t.Fatalf("expected paraphrase similarity %.3f to meet threshold %.2f", similarity, titleSimilarityThreshold)
	}
	if similarity := titleSimilarity("AI新モデル発表", "AI旧制度発表"); similarity >= titleSimilarityThreshold {
		t.Fatalf("expected unrelated similarity %.3f to stay below threshold %.2f", similarity, titleSimilarityThreshold)
	}
}
