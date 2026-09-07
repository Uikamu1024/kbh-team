package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"backend/internal/db"
	"backend/internal/debuglog"
	"backend/internal/domain"
	"backend/internal/pipeline"
)

// Stage values reported by GET /api/users/{userId}/generation-status. They
// alias the same three-item checklist already shown in
// frontend/src/pages/Onboarding.tsx (GENERATING_STATUS_MESSAGES), so no new
// UI copy is needed — the frontend just maps these onto that checklist.
const (
	stageCollecting = "collecting" // fetch → dedupe → score/select
	stageScripting  = "scripting"  // LLM script generation
	stageFinishing  = "finishing"  // TTS synthesis + audio save + DB persist
)

// generationRecord is the in-memory state for one user's in-flight or
// most-recently-finished generation (see Server.generatingUsers). It backs
// both the ALREADY_GENERATING lock and GET .../generation-status.
type generationRecord struct {
	generating bool
	stage      string
	// lastError is set only when the most recent attempt (since this record
	// was created by startGenerating) finished with an error, so a client
	// that was disconnected mid-generation (page reload, tab close) and
	// comes back can still learn why it failed instead of just seeing
	// "not generating" with no explanation. Cleared on the next startGenerating.
	lastError *generationError
}

type generationError struct {
	code    string
	message string
}

// regenerateAcceptedResponse is returned immediately (202) once the request
// is validated and the in-memory lock is acquired; the actual pipeline runs
// in the background (see runRegenerate) so a client disconnect (page reload,
// tab close, navigating away) does not cancel it — only GET
// .../generation-status, which the frontend polls, reports how it's going
// and how it ultimately turned out.
type regenerateAcceptedResponse struct {
	AcceptedAt time.Time `json:"acceptedAt"`
}

// regenerateLatestProgram starts generating and persisting a new program for
// the user, fetching articles live for user.Tags rather than reading a
// pre-built cache (see pipeline.FetchArticles), so the very first program
// right after onboarding — and any later "作り直す" — is generated on the
// spot from current RSS content, with no dependency on cmd/ingest having run
// first. It does NOT replace or delete the existing latest program — it adds
// a new one to the history. This endpoint doubles as both "generate today's
// first program" (frontend/src/lib/useGenerateProgram.ts, called right after
// onboarding and from the home screen's "今すぐ生成する") and "今日の番組を
// リセット" (Profile画面): the daily reset-count limit (IncrementResetCount,
// 3/day) is only charged when the user already has a program from today —
// see the HasProgramSince check in runRegenerate — so the first generation
// of the day never consumes it.
//
// The handler itself only does the fast, synchronous checks (user exists,
// not already generating) and then hands off to a background goroutine
// (runRegenerate) using its own context — deliberately NOT r.Context(),
// so that this generation keeps running even if the client that started it
// disconnects (verified in production: a page reload aborted the client's
// fetch, which canceled r.Context(), which then genuinely canceled the
// in-flight pipeline — the polling status display was accurately reporting
// a real stoppage, not lying). The in-memory lock/state is intentionally
// process-local, matching the single backend process deployment assumed by
// this project.
func (s *Server) regenerateLatestProgram(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	user, err := s.database.GetUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザーを取得できません")
		return
	}

	if !s.startGenerating(userID) {
		writeError(w, http.StatusConflict, "ALREADY_GENERATING", "前回のリクエストを処理中です。しばらく待ってから再度お試しください")
		return
	}

	writeJSON(w, http.StatusAccepted, regenerateAcceptedResponse{AcceptedAt: time.Now().UTC()})

	go s.runRegenerate(user)
}

// runRegenerate does the actual work for regenerateLatestProgram, in the
// background. Every exit path calls either s.finishGenerating (success) or
// s.failGenerating (error, recorded so a polling client can learn why) —
// there is no unwinding writeError/writeJSON here since the HTTP response
// was already sent by the caller.
func (s *Server) runRegenerate(user db.User) {
	userID := user.ID
	defer func() {
		// finishGenerating/failGenerating are called explicitly on every
		// path below; this recover only guards against a genuine panic
		// leaving the lock held forever.
		if p := recover(); p != nil {
			s.failGenerating(userID, "INTERNAL_ERROR", "番組の生成中に予期しないエラーが発生しました")
			panic(p)
		}
	}()

	requestStartedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// A previous program is optional: previousTopics is just used for
	// IsNew/changeCount diffing, and there being no program yet (first-ever
	// generation) is not an error now that this no longer requires an
	// existing program to replace.
	previousTopics := []string(nil)
	if _, previousChapters, err := s.database.GetLatestProgramByUser(ctx, userID); err == nil {
		previousTopics = make([]string, 0, len(previousChapters))
		for _, chapter := range previousChapters {
			previousTopics = append(previousTopics, chapter.Title)
		}
	} else if !errors.Is(err, db.ErrProgramNotFound) {
		s.failGenerating(userID, "INTERNAL_ERROR", "既存の番組を取得できません")
		return
	}

	// 記事はキャッシュからではなく、その場でタグに合うRSSフィードから生きた
	// 記事を取得する（cmd/demoの軽量パイプラインと同じ経路）。ユーザーが
	// テーマを選んだその場で番組を生成・再生できるようにするため、事前の
	// cmd/ingestバッチに依存しない。
	stageStartedAt := time.Now()
	articles, err := pipeline.FetchArticles(ctx, s.articleFetcher, user.Tags)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: fetch articles failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		s.failGenerating(userID, "UPSTREAM_FETCH_FAILED", "記事の取得に失敗しました")
		return
	}
	topics := pipeline.DedupeArticles(articles)
	selected, changeCount, err := pipeline.ScoreAndSelect(ctx, s.languageModel, topics, user.LengthMinutes, previousTopics)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: score and select topics failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		s.failGenerating(userID, "UPSTREAM_LLM_FAILED", "トピックの選定に失敗しました")
		return
	}
	if len(selected) == 0 {
		debuglog.Printf("api: regenerate: user %s: no topics found for tags %v after %s", userID, user.Tags, time.Since(stageStartedAt).Round(time.Millisecond))
		s.failGenerating(userID, "ARTICLE_CACHE_EMPTY", "選んだテーマの記事が見つかりませんでした")
		return
	}
	debuglog.Printf("api: regenerate: user %s: selected %d chapters in %s", userID, len(selected), time.Since(stageStartedAt).Round(time.Millisecond))

	today, err := deliveryDate(time.Now(), user.DeliveryTime)
	if err != nil {
		s.failGenerating(userID, "INTERNAL_ERROR", "配信日の計算に失敗しました")
		return
	}
	// その日の最初の1本（オンボーディング直後・ホームの「今すぐ生成する」）は
	// 「作り直し」ではないため、1日3回までのリセット上限にカウントしない。
	// 既にその日の番組がある状態でこのエンドポイントが呼ばれたときだけ、
	// 本来の「今日の番組をリセット」として上限を消費する。
	hasProgramToday, err := s.database.HasProgramSince(ctx, userID, today)
	if err != nil {
		s.failGenerating(userID, "INTERNAL_ERROR", "本日の番組の有無を確認できません")
		return
	}
	if hasProgramToday {
		if _, err := s.database.IncrementResetCount(ctx, userID, today); err != nil {
			code, message := resetErrorCodeAndMessage(err)
			s.failGenerating(userID, code, message)
			return
		}
	}

	stageStartedAt = time.Now()
	s.setGeneratingStage(userID, stageScripting)
	greetingText, drafts, err := pipeline.GenerateScript(ctx, s.languageModel, selected)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: generate script failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		s.failGenerating(userID, "UPSTREAM_LLM_FAILED", "台本生成に失敗しました")
		return
	}
	debuglog.Printf("api: regenerate: user %s: generated script for %d chapters in %s", userID, len(drafts), time.Since(stageStartedAt).Round(time.Millisecond))

	stageStartedAt = time.Now()
	s.setGeneratingStage(userID, stageFinishing)
	chapters, err := pipeline.SynthesizeChapters(ctx, s.speechSynthesizer, greetingText, drafts)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: synthesize chapters failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		s.failGenerating(userID, "UPSTREAM_TTS_FAILED", "音声生成に失敗しました")
		return
	}
	debuglog.Printf("api: regenerate: user %s: synthesized %d chapters in %s (total so far %s)", userID, len(chapters), time.Since(stageStartedAt).Round(time.Millisecond), time.Since(requestStartedAt).Round(time.Millisecond))
	if s.storage == nil {
		s.failGenerating(userID, "INTERNAL_ERROR", "音声ストレージが設定されていません")
		return
	}

	programID, chapterIDs, chapterAudioPaths, err := s.saveGeneratedAudio(chapters)
	if err != nil {
		s.failGenerating(userID, "INTERNAL_ERROR", "生成した音声を保存できません")
		return
	}
	if err := s.database.CreateProgramWithIDsAndSeenTopics(ctx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, pipeline.TopicGroupIDs(selected)); err != nil {
		_ = s.storage.Delete(programID)
		s.failGenerating(userID, "INTERNAL_ERROR", "番組を保存できません")
		return
	}

	debuglog.Printf("api: regenerate: user %s: done in %s", userID, time.Since(requestStartedAt).Round(time.Millisecond))
	s.finishGenerating(userID)
}

// startGenerating acquires the per-user generation lock, discarding any
// lastError recorded by a previous attempt (a fresh attempt supersedes it).
func (s *Server) startGenerating(userID string) bool {
	s.generatingMu.Lock()
	defer s.generatingMu.Unlock()
	if s.generatingUsers == nil {
		s.generatingUsers = make(map[string]*generationRecord)
	}
	if rec, exists := s.generatingUsers[userID]; exists && rec.generating {
		return false
	}
	s.generatingUsers[userID] = &generationRecord{generating: true, stage: stageCollecting}
	return true
}

// finishGenerating marks a successful completion and releases the record —
// there is nothing left for a polling client to learn once the program is
// saved (it can be fetched normally via GET .../programs/latest).
func (s *Server) finishGenerating(userID string) {
	s.generatingMu.Lock()
	delete(s.generatingUsers, userID)
	s.generatingMu.Unlock()
}

// failGenerating marks a failed completion, keeping the record (with
// generating=false) so GET .../generation-status can report why the most
// recent attempt failed to a client that reconnects after being disconnected
// mid-generation. It's superseded the next time startGenerating runs.
func (s *Server) failGenerating(userID, code, message string) {
	s.generatingMu.Lock()
	s.generatingUsers[userID] = &generationRecord{
		generating: false,
		lastError:  &generationError{code: code, message: message},
	}
	s.generatingMu.Unlock()
}

// setGeneratingStage updates the in-progress stage for a user that is
// already generating. No-op if the key isn't present or generation already
// finished.
func (s *Server) setGeneratingStage(userID, stage string) {
	s.generatingMu.Lock()
	if rec, exists := s.generatingUsers[userID]; exists && rec.generating {
		rec.stage = stage
	}
	s.generatingMu.Unlock()
}

// generationStatus reports whether userID currently has a generation in
// flight, its stage if so, and — if not, and the most recent attempt
// failed — why.
func (s *Server) generationStatus(userID string) (generating bool, stage string, lastError *generationError) {
	s.generatingMu.Lock()
	defer s.generatingMu.Unlock()
	rec, ok := s.generatingUsers[userID]
	if !ok {
		return false, "", nil
	}
	return rec.generating, rec.stage, rec.lastError
}

func (s *Server) saveGeneratedAudio(chapters []domain.ChapterAudio) (string, []string, []string, error) {
	programID, err := newAPIUUID()
	if err != nil {
		return "", nil, nil, err
	}
	chapterIDs := make([]string, len(chapters))
	chapterAudioPaths := make([]string, len(chapters))
	for index, chapter := range chapters {
		chapterID, err := newAPIUUID()
		if err != nil {
			_ = s.storage.Delete(programID)
			return "", nil, nil, err
		}
		path, err := s.storage.Save(programID, chapterID, chapter.AudioBytes)
		if err != nil {
			_ = s.storage.Delete(programID)
			return "", nil, nil, err
		}
		chapterIDs[index] = chapterID
		chapterAudioPaths[index] = path
	}
	return programID, chapterIDs, chapterAudioPaths, nil
}

func resetErrorCodeAndMessage(err error) (string, string) {
	switch {
	case errors.Is(err, db.ErrUserNotFound):
		return "USER_NOT_FOUND", "指定されたユーザーが見つかりません"
	case errors.Is(err, db.ErrResetLimitExceeded):
		return "RESET_LIMIT_EXCEEDED", "本日の作り直し回数の上限（3回）に達しています"
	default:
		return "INTERNAL_ERROR", "作り直し回数を更新できません"
	}
}

func deliveryDate(now time.Time, deliveryTime string) (time.Time, error) {
	delivery, err := time.Parse("15:04", deliveryTime)
	if err != nil {
		return time.Time{}, err
	}
	currentMinutes := now.Hour()*60 + now.Minute()
	deliveryMinutes := delivery.Hour()*60 + delivery.Minute()
	if currentMinutes < deliveryMinutes {
		now = now.AddDate(0, 0, -1)
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
}
