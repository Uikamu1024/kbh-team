// 通学ラジオ — UIモック（pure JS、状態はすべてメモリ内のダミーデータ）
// データ形状は docs/api-contract.yaml の Program / Chapter スキーマに寄せている

(() => {
  "use strict";

  // ===== ダミーデータ =====
  const PROGRAM = {
    id: "8f14e45f-ceea-4c9e-8b52-1c4d5d5f6a12",
    createdAt: "2026-08-29T06:00:00+09:00",
    chapters: [
      {
        id: "c1", position: 0, title: "AI開発の新モデルが発表、性能が前世代比2倍に",
        sourceUrl: "https://example.com/ai-model", sourceName: "example.com",
        durationSec: 95,
        lines: [
          "おはようございます。今日はAI関連で3件動きがあります。",
          "まず注目は新しい言語モデルの発表です。",
          "前世代と比べて推論速度が2倍になったとのことです。",
          "複数のメディアが同時に取り上げていて、注目度の高さがうかがえます。",
        ],
      },
      {
        id: "c2", position: 1, title: "京都市、観光客向けAIガイドアプリを試験導入",
        sourceUrl: "https://example.com/kyoto-ai-guide", sourceName: "kyoto-news.example.com",
        durationSec: 82,
        lines: [
          "続いては京都からのニュースです。",
          "市がAIを使った観光ガイドアプリの実証実験を始めました。",
          "多言語対応で、外国人観光客からの反応も良いようです。",
        ],
      },
      {
        id: "c3", position: 2, title: "国内スタートアップが新作ゲームエンジンを公開",
        sourceUrl: "https://example.com/game-engine", sourceName: "game-media.example.com",
        durationSec: 110,
        lines: [
          "ゲーム業界のニュースです。",
          "国内スタートアップが独自開発のゲームエンジンをオープンソースで公開しました。",
          "軽量さを売りにしていて、インディー開発者からの期待が高まっています。",
        ],
      },
      {
        id: "c4", position: 3, title: "大学発ベンチャー、対話型AIの実証実験を開始",
        sourceUrl: "https://example.com/univ-ai", sourceName: "example.com",
        durationSec: 76,
        lines: [
          "続いての話題です。",
          "大学発のベンチャー企業が対話型AIの実証実験を始めました。",
          "教育現場での活用を目指しているとのことです。",
        ],
      },
      {
        id: "c5", position: 4, title: "週末は京都でテクノロジーイベントが開催予定",
        sourceUrl: "https://example.com/kyoto-tech-event", sourceName: "kyoto-news.example.com",
        durationSec: 68,
        lines: [
          "最後は週末のイベント情報です。",
          "京都市内でテクノロジー関連のイベントが開催されます。",
          "それでは今日も良い一日を。行ってらっしゃい。",
        ],
      },
    ],
  };
  PROGRAM.totalDurationSec = PROGRAM.chapters.reduce((s, c) => s + c.durationSec, 0);

  const HISTORY = [
    { date: "8月28日(金)", title: "円安基調が継続、輸出企業への影響は", duration: "11分" },
    { date: "8月27日(木)", title: "京都の紅葉スポット、今年の見頃予測", duration: "9分" },
    { date: "8月26日(水)", title: "新作ゲーム発表ラッシュ、注目タイトルまとめ", duration: "13分" },
  ];

  const PRESET_TAGS = ["AI", "京都", "ゲーム", "音楽", "スポーツ", "ビジネス", "映画", "旅行"];
  const MAX_TAGS = 3;
  const ONBOARDING_STORAGE_KEY = "tsugaku-radio-onboarding";
  let selectedTags = [];
  let userName = "ゲスト";

  // ===== 状態 =====
  const state = {
    screen: "onboarding-name",
    currentChapterIndex: 0,
    isPlaying: false,
    progressRatio: 0.0, // 現在チャプター内の再生位置(0-1)
    lyricIndex: 0,
  };

  let waveformBars = [];
  let tickTimer = null;
  let lyricTimer = null;

  // ===== DOM参照 =====
  const $ = (sel) => document.querySelector(sel);
  const screens = {
    name: $("#screen-onboarding-name"),
    interests: $("#screen-onboarding-interests"),
    generating: $("#screen-generating"),
    home: $("#screen-home"),
    player: $("#screen-player"),
    profile: $("#screen-profile"),
  };
  const navItems = document.querySelectorAll(".nav-item");
  const minimizeBtn = $("#minimizeBtn");
  const bottomNav = $(".bottom-nav");
  const toastEl = $("#toast");
  const resetOnboardingBtn = $("#resetOnboardingBtn");

  // ===== 画面遷移 =====
  function showScreen(name) {
    state.screen = name;
    Object.entries(screens).forEach(([key, el]) => {
      el.hidden = key !== name;
    });
    navItems.forEach((btn) => btn.classList.toggle("active", btn.dataset.target === name));
    minimizeBtn.hidden = name !== "player";
    bottomNav.hidden = name === "name" || name === "interests";
    renderHeroPlayState();
  }

  navItems.forEach((btn) => {
    btn.addEventListener("click", () => showScreen(btn.dataset.target));
  });
  minimizeBtn.addEventListener("click", () => showScreen("home"));
  resetOnboardingBtn.addEventListener("click", () => {
    localStorage.removeItem(ONBOARDING_STORAGE_KEY);
    location.reload();
  });

  // ===== 初回設定 =====
  const nameForm = $("#nameForm");
  const userNameInput = $("#userNameInput");
  const nameNextBtn = $("#nameNextBtn");
  const onboardingTagGrid = $("#onboardingTagGrid");
  const onboardingCounter = $("#onboardingCounter");
  const interestsNextBtn = $("#interestsNextBtn");
  let generationTimer = null;

  userNameInput.addEventListener("input", () => {
    nameNextBtn.disabled = userNameInput.value.trim().length === 0;
  });

  nameForm.addEventListener("submit", (event) => {
    event.preventDefault();
    userName = userNameInput.value.trim();
    renderOnboardingTags();
    showScreen("interests");
  });

  function renderOnboardingTags() {
    onboardingTagGrid.innerHTML = "";
    PRESET_TAGS.forEach((tag) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "onboarding-tag" + (selectedTags.includes(tag) ? " selected" : "");
      button.textContent = tag;
      button.setAttribute("aria-pressed", selectedTags.includes(tag));
      button.disabled = selectedTags.length >= MAX_TAGS && !selectedTags.includes(tag);
      button.addEventListener("click", () => {
        if (selectedTags.includes(tag)) {
          selectedTags = selectedTags.filter((item) => item !== tag);
        } else if (selectedTags.length < MAX_TAGS) {
          selectedTags = [...selectedTags, tag];
        }
        renderOnboardingTags();
      });
      onboardingTagGrid.appendChild(button);
    });
    onboardingCounter.textContent = `${selectedTags.length} / ${MAX_TAGS} 選択中`;
    interestsNextBtn.disabled = selectedTags.length < 2;
  }

  interestsNextBtn.addEventListener("click", () => {
    applyUserProfile();
    localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify({ name: userName, tags: selectedTags }));
    $("#generatingTopics").textContent = selectedTags.join("・");
    showScreen("generating");
    startGenerationPreview();
  });

  function startGenerationPreview() {
    clearTimeout(generationTimer);
    const statusItems = document.querySelectorAll(".generating-status-item");
    statusItems.forEach((item, index) => item.classList.toggle("active", index === 0));
    generationTimer = setTimeout(() => {
      statusItems.forEach((item, index) => item.classList.toggle("active", index <= 1));
    }, 650);
    generationTimer = setTimeout(() => {
      statusItems.forEach((item) => item.classList.add("active"));
    }, 1250);
    generationTimer = setTimeout(() => {
      renderTagGrid();
      showScreen("home");
    }, 1900);
  }

  function applyUserProfile() {
    $("#greetingName").textContent = userName;
    $("#profileName").textContent = `${userName}さん`;
    $("#profileAvatar").textContent = userName.slice(0, 1).toUpperCase();
    renderHomeTags();
  }

  function renderHomeTags() {
    const homeTags = $(".home-tags");
    homeTags.innerHTML = selectedTags.map((tag) => `<span class="home-tag">${tag}</span>`).join("");
  }

  // ===== トースト =====
  let toastTimer = null;
  function showToast(message) {
    toastEl.textContent = message;
    toastEl.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toastEl.classList.remove("show"), 1800);
  }

  // ===== ホーム画面 =====
  function renderHistory() {
    const listEl = $("#historyList");
    listEl.innerHTML = "";
    HISTORY.forEach((item) => {
      const li = document.createElement("li");
      li.className = "history-item";
      li.innerHTML = `
        <div class="history-thumb">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
        </div>
        <div class="history-text">
          <p class="history-date">${item.date}</p>
          <p class="history-title">${item.title}</p>
        </div>
        <span class="history-duration">${item.duration}</span>
      `;
      li.addEventListener("click", () => showToast("過去の番組の再生はモック対象外です"));
      listEl.appendChild(li);
    });
  }

  function renderHeroPlayState() {
    const button = $("#heroPlayBtn");
    const icon = button.querySelector("svg");
    const isPlaying = state.isPlaying;
    button.classList.toggle("playing", isPlaying);
    button.setAttribute("aria-label", isPlaying ? "今日の番組を一時停止" : "今日の番組を再生");
    icon.innerHTML = isPlaying
      ? '<path d="M7 5h4v14H7zM13 5h4v14h-4z"/>'
      : '<path d="M8 5v14l11-7z"/>';
  }

  $("#heroPlayBtn").addEventListener("click", () => {
    if (state.isPlaying) {
      togglePlay();
      return;
    }
    showScreen("player");
    togglePlay();
  });

  // ===== プレイヤー画面 =====
  function currentChapter() {
    return PROGRAM.chapters[state.currentChapterIndex];
  }

  function buildWaveform() {
    const wf = $("#waveform");
    wf.innerHTML = "";
    waveformBars = [];
    const barCount = 56;
    // 記事の長さっぽく緩やかに山を作る疑似波形
    for (let i = 0; i < barCount; i++) {
      const t = i / barCount;
      const envelope = 0.35 + 0.65 * Math.sin(Math.PI * t) ** 0.6;
      const jitter = 0.55 + Math.random() * 0.45;
      const height = Math.max(4, Math.round(envelope * jitter * 32));
      const bar = document.createElement("div");
      bar.className = "bar";
      bar.style.height = `${height}px`;
      wf.appendChild(bar);
      waveformBars.push(bar);
    }
    wf.onclick = (e) => {
      const rect = wf.getBoundingClientRect();
      const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
      state.progressRatio = ratio;
      renderProgress();
    };
  }

  function formatTime(sec) {
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${String(s).padStart(2, "0")}`;
  }

  function renderProgress() {
    const chapter = currentChapter();
    const playedCount = Math.round(waveformBars.length * state.progressRatio);
    waveformBars.forEach((bar, i) => bar.classList.toggle("played", i < playedCount));
    $("#timeCurrent").textContent = formatTime(chapter.durationSec * state.progressRatio);
    $("#timeTotal").textContent = formatTime(chapter.durationSec);

    // 歌詞ハイライトを進捗に合わせて切り替える
    const lyricLines = document.querySelectorAll("#lyrics .line");
    const idx = Math.min(lyricLines.length - 1, Math.floor(state.progressRatio * lyricLines.length));
    lyricLines.forEach((el, i) => el.classList.toggle("active", i === idx));
  }

  function renderLyrics() {
    const lyricsEl = $("#lyrics");
    lyricsEl.innerHTML = "";
    currentChapter().lines.forEach((text) => {
      const div = document.createElement("div");
      div.className = "line";
      div.textContent = text;
      lyricsEl.appendChild(div);
    });
  }

  function renderPlaylist() {
    const listEl = $("#playlistList");
    listEl.innerHTML = "";
    PROGRAM.chapters.forEach((chapter, i) => {
      const li = document.createElement("li");
      li.className = "playlist-item" + (i === state.currentChapterIndex ? " current" : "");
      const leftHtml = i === state.currentChapterIndex
        ? `<div class="playlist-eq"><span></span><span></span><span></span></div>`
        : `<span class="playlist-index">${i + 1}</span>`;
      li.innerHTML = `
        ${leftHtml}
        <div class="playlist-text">
          <p class="playlist-title">${chapter.title}</p>
          <p class="playlist-source">${chapter.sourceName}</p>
        </div>
        <span class="playlist-duration">${formatTime(chapter.durationSec)}</span>
      `;
      li.addEventListener("click", () => loadChapter(i));
      listEl.appendChild(li);
    });
  }

  function loadChapter(index) {
    state.currentChapterIndex = (index + PROGRAM.chapters.length) % PROGRAM.chapters.length;
    state.progressRatio = 0;
    const chapter = currentChapter();
    $("#trackDomain").textContent = chapter.sourceName;
    $("#trackTitle").textContent = chapter.title;
    $("#trackProgress").textContent = `${state.currentChapterIndex + 1} / ${PROGRAM.chapters.length}`;
    renderLyrics();
    renderProgress();
    renderPlaylist();
  }

  function setPlayIcon(playing) {
    const icon = $("#playIcon");
    icon.innerHTML = playing
      ? '<path d="M7 5h4v14H7zM13 5h4v14h-4z"/>'
      : '<path d="M8 5v14l11-7z"/>';
  }

  function togglePlay() {
    state.isPlaying = !state.isPlaying;
    setPlayIcon(state.isPlaying);
    renderHeroPlayState();
    if (state.isPlaying) startTicking(); else stopTicking();
  }

  function startTicking() {
    stopTicking();
    tickTimer = setInterval(() => {
      const chapter = currentChapter();
      state.progressRatio += 1 / chapter.durationSec / 4; // 0.25秒刻みの疑似進行
      if (state.progressRatio >= 1) {
        loadChapter(state.currentChapterIndex + 1);
        return;
      }
      renderProgress();
    }, 250);
  }

  function stopTicking() {
    clearInterval(tickTimer);
    tickTimer = null;
  }

  $("#playBtn").addEventListener("click", togglePlay);
  $("#prevBtn").addEventListener("click", () => loadChapter(state.currentChapterIndex - 1));
  $("#nextBtn").addEventListener("click", () => loadChapter(state.currentChapterIndex + 1));

  // ===== プロフィール画面 =====
  function renderTagGrid() {
    const grid = $("#tagGrid");
    grid.innerHTML = "";
    PRESET_TAGS.forEach((tag) => {
      const btn = document.createElement("button");
      btn.className = "tag-chip" + (selectedTags.includes(tag) ? " selected" : "");
      btn.type = "button";
      btn.textContent = tag;
      const atLimit = selectedTags.length >= MAX_TAGS && !selectedTags.includes(tag);
      btn.disabled = atLimit;
      btn.addEventListener("click", () => {
        if (selectedTags.includes(tag)) {
          selectedTags = selectedTags.filter((t) => t !== tag);
        } else if (selectedTags.length < MAX_TAGS) {
          selectedTags = [...selectedTags, tag];
        }
        renderTagGrid();
        showToast(`テーマを更新しました（${selectedTags.join("、") || "未選択"}）`);
      });
      grid.appendChild(btn);
    });
  }

  const tagEditor = $("#tagEditor");
  $("#tagSettingBtn").addEventListener("click", () => {
    tagEditor.hidden = !tagEditor.hidden;
    if (!tagEditor.hidden) renderTagGrid();
  });

  // ===== 設定（配信時刻・番組の長さ・今日の番組リセット） =====
  const RESET_LIMIT = 3;
  let resetCount = 0;

  $("#deliveryTime").addEventListener("change", (e) => {
    showToast(`配信時刻を${e.target.value}に設定しました`);
  });

  const lengthSegmented = $("#lengthSegmented");
  lengthSegmented.addEventListener("click", (e) => {
    const btn = e.target.closest(".segmented-btn");
    if (!btn) return;
    lengthSegmented.querySelectorAll(".segmented-btn").forEach((b) => b.classList.toggle("selected", b === btn));
    showToast(`番組の長さを${btn.dataset.value}分に設定しました`);
  });

  const resetBtn = $("#resetBtn");
  const resetDesc = $("#resetDesc");
  function renderResetState() {
    const remaining = RESET_LIMIT - resetCount;
    resetDesc.textContent = remaining > 0
      ? `今日の番組を作り直します（残り${remaining}回）`
      : "本日の上限に達しました（明日6:00にリセット）";
    resetBtn.disabled = remaining <= 0;
  }
  resetBtn.addEventListener("click", () => {
    if (resetCount >= RESET_LIMIT) return;
    resetCount += 1;
    renderResetState();
    showToast("今日の番組を作り直しています…");
  });
  renderResetState();

  // ===== 初期化 =====
  function init() {
    $("#greetingCount").textContent = String(PROGRAM.chapters.length);
    $("#heroCaption").textContent =
      `今日の番組・${PROGRAM.chapters.length}チャプター・約${Math.round(PROGRAM.totalDurationSec / 60)}分`;
    renderHistory();
    buildWaveform();
    loadChapter(0);
    const savedProfile = JSON.parse(localStorage.getItem(ONBOARDING_STORAGE_KEY) || "null");
    if (savedProfile && savedProfile.name && Array.isArray(savedProfile.tags)) {
      userName = savedProfile.name;
      selectedTags = savedProfile.tags.slice(0, MAX_TAGS);
      applyUserProfile();
      renderTagGrid();
      showScreen("home");
    } else {
      renderOnboardingTags();
      showScreen("name");
    }
    renderHeroPlayState();
  }

  init();
})();
