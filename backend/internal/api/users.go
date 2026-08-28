package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/db"
)

type createUserResponse struct {
	UserID string `json:"userId"`
}

type userProfileResponse struct {
	UserID            string    `json:"userId"`
	Tags              []string  `json:"tags"`
	DeliveryTime      string    `json:"deliveryTime"`
	LengthMinutes     int       `json:"lengthMinutes"`
	ResetsUsedToday   int       `json:"resetsUsedToday"`
	ResetsLimitPerDay int       `json:"resetsLimitPerDay"`
	Stats             userStats `json:"stats"`
}

type userStats struct {
	TotalPrograms    int `json:"totalPrograms"`
	TotalDurationSec int `json:"totalDurationSec"`
}

type tagsRequest struct {
	Tags []string `json:"tags"`
}

type settingsRequest struct {
	DeliveryTime  string `json:"deliveryTime"`
	LengthMinutes int    `json:"lengthMinutes"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	userID, err := s.database.CreateUser(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザーを作成できません")
		return
	}
	writeJSON(w, http.StatusCreated, createUserResponse{UserID: userID})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
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

	summaries, total, err := s.database.ListProgramsByUser(r.Context(), userID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー統計を取得できません")
		return
	}
	totalDuration := 0
	for _, summary := range summaries {
		totalDuration += summary.TotalDurationSec
	}
	resetsUsed := user.ResetCount
	if user.ResetDate == nil || !sameDate(*user.ResetDate, time.Now()) {
		resetsUsed = 0
	}
	writeJSON(w, http.StatusOK, userProfileResponse{
		UserID:            user.ID,
		Tags:              user.Tags,
		DeliveryTime:      user.DeliveryTime,
		LengthMinutes:     user.LengthMinutes,
		ResetsUsedToday:   resetsUsed,
		ResetsLimitPerDay: 3,
		Stats:             userStats{TotalPrograms: total, TotalDurationSec: totalDuration},
	})
}

func (s *Server) updateUserTags(w http.ResponseWriter, r *http.Request) {
	var request tagsRequest
	if !decodeJSON(w, r, &request, "INVALID_TAGS", "リクエストJSONの形式が不正です") {
		return
	}
	if len(request.Tags) < 1 || len(request.Tags) > 3 {
		writeError(w, http.StatusBadRequest, "INVALID_TAGS", "tagsは1〜3件で指定してください")
		return
	}
	for index, tag := range request.Tags {
		request.Tags[index] = strings.TrimSpace(tag)
		if request.Tags[index] == "" {
			writeError(w, http.StatusBadRequest, "INVALID_TAGS", "tagsの要素に空文字列は指定できません")
			return
		}
	}
	if err := s.database.UpdateUserTags(r.Context(), r.PathValue("userId"), request.Tags); err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザーのタグを更新できません")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updateUserSettings(w http.ResponseWriter, r *http.Request) {
	var request settingsRequest
	if !decodeJSON(w, r, &request, "INVALID_SETTINGS", "リクエストJSONの形式が不正です") {
		return
	}
	if !validDeliveryTime(request.DeliveryTime) || !validLengthMinutes(request.LengthMinutes) {
		writeError(w, http.StatusBadRequest, "INVALID_SETTINGS", "deliveryTimeはHH:MM形式、lengthMinutesは5・10・15のいずれかで指定してください")
		return
	}
	if err := s.database.UpdateUserSettings(r.Context(), r.PathValue("userId"), request.DeliveryTime, request.LengthMinutes); err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー設定を更新できません")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validDeliveryTime(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	hour, hourErr := strconv.Atoi(value[:2])
	minute, minuteErr := strconv.Atoi(value[3:])
	return hourErr == nil && minuteErr == nil && hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

func validLengthMinutes(value int) bool {
	return value == 5 || value == 10 || value == 15
}

func sameDate(left, right time.Time) bool {
	leftYear, leftMonth, leftDay := left.Date()
	rightYear, rightMonth, rightDay := right.Date()
	return leftYear == rightYear && leftMonth == rightMonth && leftDay == rightDay
}
