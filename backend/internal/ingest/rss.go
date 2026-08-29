// Package ingest contains the RSS collection logic used by cmd/ingest.
package ingest

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// RSSItem is the RSS metadata needed before article-body retrieval.
type RSSItem struct {
	Title          string
	Link           string
	PublishedAt    time.Time
	HasPublishedAt bool
}

type xmlRSSItem struct {
	Title   string  `xml:"title"`
	Link    xmlLink `xml:"link"`
	PubDate string  `xml:"pubDate"`
	DCDate  string  `xml:"date"`
}

type xmlLink struct {
	Value    string `xml:",chardata"`
	Resource string `xml:"resource,attr"`
}

// ParseRSS walks item elements independently of whether they are nested in an
// RSS 2.0 channel or directly under an RDF/RSS 1.0 document.
func ParseRSS(reader io.Reader) ([]RSSItem, error) {
	decoder := xml.NewDecoder(reader)
	items := make([]RSSItem, 0)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return items, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse XML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "item" {
			continue
		}

		var parsed xmlRSSItem
		if err := decoder.DecodeElement(&parsed, &start); err != nil {
			return nil, fmt.Errorf("parse item: %w", err)
		}
		link := strings.TrimSpace(parsed.Link.Value)
		if link == "" {
			link = strings.TrimSpace(parsed.Link.Resource)
		}
		item := RSSItem{
			Title: strings.TrimSpace(parsed.Title),
			Link:  link,
		}
		if publishedAt, ok := parseRSSDate(parsed.PubDate); ok {
			item.PublishedAt = publishedAt
			item.HasPublishedAt = true
		} else if publishedAt, ok := parseRSSDate(parsed.DCDate); ok {
			item.PublishedAt = publishedAt
			item.HasPublishedAt = true
		}
		items = append(items, item)
	}
}

func parseRSSDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC850, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
