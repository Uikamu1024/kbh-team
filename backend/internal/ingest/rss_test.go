package ingest

import (
	"strings"
	"testing"
	"time"
)

func TestParseRSS2Items(t *testing.T) {
	items, err := ParseRSS(strings.NewReader(`<?xml version="1.0"?>
<rss><channel><item><title>RSS title</title><link>https://example.com/rss</link><pubDate>Mon, 02 Jan 2006 15:04:05 +0900</pubDate></item></channel></rss>`))
	if err != nil {
		t.Fatalf("ParseRSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}
	if items[0].Title != "RSS title" || items[0].Link != "https://example.com/rss" || !items[0].HasPublishedAt {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	if !items[0].PublishedAt.Equal(time.Date(2006, time.January, 2, 6, 4, 5, 0, time.UTC)) {
		t.Fatalf("published at = %s", items[0].PublishedAt)
	}
}

func TestParseRDFItemsUsingDCDate(t *testing.T) {
	items, err := ParseRSS(strings.NewReader(`<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:dc="http://purl.org/dc/elements/1.1/"><item><title>RDF title</title><link rdf:resource="https://example.com/rdf"/><dc:date>2026-08-29T01:02:03Z</dc:date></item></rdf:RDF>`))
	if err != nil {
		t.Fatalf("ParseRSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}
	if items[0].Title != "RDF title" || items[0].Link != "https://example.com/rdf" || !items[0].HasPublishedAt {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestParseRSSKeepsMissingPublicationDate(t *testing.T) {
	items, err := ParseRSS(strings.NewReader(`<rss><channel><item><title>undated</title><link>https://example.com/undated</link></item></channel></rss>`))
	if err != nil {
		t.Fatalf("ParseRSS: %v", err)
	}
	if len(items) != 1 || items[0].HasPublishedAt {
		t.Fatalf("unexpected items: %+v", items)
	}
}
