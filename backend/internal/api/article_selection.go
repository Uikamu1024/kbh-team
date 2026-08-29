package api

import (
	"errors"
	"net/http"

	"backend/internal/pipeline"
)

func writeArticleSelectionError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, pipeline.ErrArticleCacheEmpty):
		writeError(w, http.StatusServiceUnavailable, "ARTICLE_CACHE_EMPTY", "利用できる新着記事がまだ収集されていません")
		return true
	case errors.Is(err, pipeline.ErrNoUnseenArticles):
		writeError(w, http.StatusServiceUnavailable, "NO_UNSEEN_ARTICLES", "未配信の新着記事がありません")
		return true
	default:
		return false
	}
}
