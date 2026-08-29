package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"backend/internal/providers/fetcher"
)

// minimumPlainTextRunes mirrors cmd/ingest's minimumBodyRunes threshold: a
// direct fetch that returns less than this is treated as a failure, falling
// back to jina.ai rather than feeding near-empty text to the LLM.
const minimumPlainTextRunes = 100

// maxPlainTextBytes caps how much of a response body we read directly, since
// this is just LLM brainstorming material, not stored article content.
const maxPlainTextBytes = 2 << 20 // 2 MiB

var (
	scriptOrStyleTagPattern = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlTagPattern          = regexp.MustCompile(`(?s)<[^>]+>`)
	whitespacePattern       = regexp.MustCompile(`\s+`)
)

// fetchArticleText retrieves article text for a URL: it first tries a plain
// direct HTTP GET (no jina.ai cost), and falls back to jinaFetcher.FetchArticle
// (the same function cmd/ingest uses) if the direct fetch fails or returns
// text too short to be useful.
func fetchArticleText(ctx context.Context, client *http.Client, jinaFetcher *fetcher.JinaFetcher, url string) (string, error) {
	if text, err := fetchPlainText(ctx, client, url); err == nil && utf8.RuneCountInString(text) >= minimumPlainTextRunes {
		return text, nil
	}

	article, err := jinaFetcher.FetchArticle(ctx, url)
	if err != nil {
		return "", err
	}
	return article.Body, nil
}

// fetchPlainText GETs url directly and extracts a crude, dependency-free text
// approximation of the page body. It is deliberately simple (regex-based tag
// stripping, no proper HTML parsing): the output is only used as LLM
// brainstorming material for tag-taxonomy proposals, not stored anywhere, so
// occasional noise is acceptable.
func fetchPlainText(ctx context.Context, client *http.Client, url string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("direct fetch returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxPlainTextBytes))
	if err != nil {
		return "", err
	}

	text := scriptOrStyleTagPattern.ReplaceAllString(string(body), " ")
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = whitespacePattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(text), nil
}
