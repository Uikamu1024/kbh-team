package llm

import (
	"strings"
	"testing"
)

func TestStripNavigationLinesRemovesPureNavLinks(t *testing.T) {
	// Representative of jina.ai Reader output for an ITmedia-hosted article:
	// several lines of pure navigation links/images before any real prose.
	body := `Title: とにかく足りない上級IT人材

URL Source: https://www.itmedia.co.jp/enterprise/articles/2608/28/news045.html

Markdown Content:
メディア

[![Image 1: ITmedia エンタープライズ](https://image.itmedia.co.jp/images/logo/pcvheader_enterprise.png)](https://www.itmedia.co.jp/enterprise/)

[記事一覧](https://www.itmedia.co.jp/enterprise/subtop/archive/)

[マイページ](https://id.itmedia.co.jp/isentry/contents?sc=abc&lc=def)

上級IT人材を確保できない企業は多く、人材育成の壁もまた高い。`

	cleaned := stripNavigationLines(body)

	if !strings.Contains(cleaned, "上級IT人材を確保できない企業は多く") {
		t.Fatalf("expected real article content to survive stripping, got: %q", cleaned)
	}
	if strings.Contains(cleaned, "image.itmedia.co.jp") || strings.Contains(cleaned, "id.itmedia.co.jp") {
		t.Fatalf("expected navigation links to be removed, got: %q", cleaned)
	}
}

func TestStripNavigationLinesKeepsPlainProse(t *testing.T) {
	body := "半導体産業を巡る投資は、もはや純粋な企業間競争を越え、経済安全保障や国家的な優先課題を背景としています。"

	if got := stripNavigationLines(body); got != body {
		t.Fatalf("expected prose without links to pass through unchanged, got: %q", got)
	}
}

func TestBodyPromptCharLimitReachesRealContentAfterNavJunk(t *testing.T) {
	navJunk := strings.Repeat("[マイページ](https://id.itmedia.co.jp/isentry/contents?sc=verylongtrackingquerystring)\n\n", 20)
	body := navJunk + "本文はここから始まります。これが実際の記事の内容です。"

	truncated := truncateRunes(stripNavigationLines(body), bodyPromptCharLimit)

	if !strings.Contains(truncated, "本文はここから始まります") {
		t.Fatalf("expected real content to be reachable within the char limit after stripping nav junk, got: %q", truncated)
	}
}
