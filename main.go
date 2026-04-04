// File: main.go
// Production-grade Telegram video downloader bot using yt-dlp
// Streams videos directly from CDN to Telegram without storing on server

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot configuration
var (
	botToken    = os.Getenv("TELEGRAM_BOT_TOKEN")
	externalURL = os.Getenv("RENDER_EXTERNAL_URL")
	cookiesFile = os.Getenv("COOKIES_FILE")
)

// Bot state
var (
	bot       *tgbotapi.BotAPI
	botDebug  = os.Getenv("DEBUG") == "true"
	userSessions = &SessionStore{
		sessions: make(map[int64]*UserSession),
	}
)

// UserSession stores the state of each user's video selection
type UserSession struct {
	VideoURL  string
	Title     string
	Duration  string
	Thumbnail string
	Formats   []VideoFormat
	Mutex     sync.RWMutex
}

// VideoFormat represents a video quality option
type VideoFormat struct {
	FormatID string
	Quality  string
	Ext      string
	Filesize int64
	URL      string
	Note     string
}

// SessionStore manages user sessions thread-safely
type SessionStore struct {
	sessions map[int64]*UserSession
	mutex    sync.RWMutex
}

// GetSession retrieves or creates a session for a user
func (s *SessionStore) GetSession(userID int64) *UserSession {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if session, exists := s.sessions[userID]; exists {
		return session
	}

	session := &UserSession{}
	s.sessions[userID] = session
	return session
}

// ClearSession removes a user's session
func (s *SessionStore) ClearSession(userID int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, userID)
}

// YtDlpOutput represents the JSON output from yt-dlp --dump-json
type YtDlpOutput struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Duration    float64 `json:"duration"`
	Thumbnail   string `json:"thumbnail"`
	Formats     []YtDlpFormat `json:"formats"`
	Description string `json:"description"`
	Uploader    string `json:"uploader"`
}

// YtDlpFormat represents a format in yt-dlp's JSON output
type YtDlpFormat struct {
	FormatID   string `json:"format_id"`
	FormatNote string `json:"format_note"`
	Ext        string `json:"ext"`
	Filesize   int64  `json:"filesize,omitempty"`
	URL        string `json:"url"`
	Height     int    `json:"height,omitempty"`
	Width      int    `json:"width,omitempty"`
	TBR        float64 `json:"tbr,omitempty"`
	FPS        float64 `json:"fps,omitempty"`
	VCodec     string `json:"vcodec,omitempty"`
	ACodec     string `json:"acodec,omitempty"`
}

// SupportedURLs contains regex patterns for supported websites
var supportedURLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:https?://)?(?:www\.)?youtube\.com/watch\?v=[\w-]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?youtu\.be/[\w-]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?instagram\.com/(?:p|reel|tv)/[\w-]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?facebook\.com/[\w./]+(?:video|watch)[\w.?=&/-]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?twitter\.com/[\w]+/status/[\d]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?x\.com/[\w]+/status/[\d]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?tiktok\.com/[\w./]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?reddit\.com/[\w./]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?twitch\.tv/[\w]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?vimeo\.com/[\d]+`),
	regexp.MustCompile(`(?:https?://)?(?:www\.)?dailymotion\.com/video/[\w]+`),
}

func main() {
	// Validate bot token
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	// Initialize bot
	var err error
	bot, err = tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	bot.Debug = botDebug
	log.Printf("Bot authorized as @%s", bot.Self.UserName)

	// Setup webhook if external URL is provided
	if externalURL != "" {
		setupWebhook()
	}

	// Start health check server
	go startHealthServer()

	// Start bot in webhook or polling mode
	if externalURL != "" {
		handleWebhookUpdates()
	} else {
		handlePollingUpdates()
	}
}

// setupWebhook configures the Telegram webhook
func setupWebhook() {
	webhookURL := strings.TrimSuffix(externalURL, "/") + "/" + bot.Token

	_, err := bot.Request(tgbotapi.NewSetWebhook(webhookURL))
	if err != nil {
		log.Printf("Failed to set webhook: %v", err)
	} else {
		log.Printf("Webhook set to: %s", webhookURL)
	}
}

// handleWebhookUpdates processes updates via webhook
func handleWebhookUpdates() {
	info, err := bot.GetWebhookInfo()
	if err != nil {
		log.Printf("Failed to get webhook info: %v", err)
	}

	if info.LastErrorDate != 0 {
		log.Printf("Telegram webhook error: %s", info.LastErrorMessage)
	}

	updates := bot.ListenForWebhook("/" + bot.Token)

	server := &http.Server{
		Addr:    ":8080",
		Handler: nil,
	}

	go func() {
		log.Println("Starting webhook server on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	for update := range updates {
		processUpdate(update)
	}
}

// handlePollingUpdates processes updates via long polling
func handlePollingUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.UpdatesChan(u)
	if err != nil {
		log.Printf("Failed to get updates channel: %v", err)
		return
	}

	// Also start health server for local testing
	go startHealthServer()

	for update := range updates {
		processUpdate(update)
	}
}

// processUpdate handles a single update from Telegram
func processUpdate(update tgbotapi.Update) {
	// Handle messages
	if update.Message != nil {
		handleMessage(update.Message)
		return
	}

	// Handle callback queries (inline keyboard buttons)
	if update.CallbackQuery != nil {
		handleCallback(update.CallbackQuery)
		return
	}
}

// handleMessage processes incoming messages
func handleMessage(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	log.Printf("[%s] Message from %s: %s", message.From.UserName, chatID, text)

	// Handle /start command
	if text == "/start" {
		handleStartCommand(chatID)
		return
	}

	// Handle /help command
	if text == "/help" {
		handleHelpCommand(chatID)
		return
	}

	// Handle URL
	if isSupportedURL(text) {
		handleVideoURL(message)
		return
	}

	// Unknown input
	msg := tgbotapi.NewMessage(chatID, "❓ Send me a video URL from YouTube, Facebook, Twitter, Instagram, TikTok, Reddit, Twitch, Vimeo, or Dailymotion.\n\nType /help for usage instructions.")
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}

// handleStartCommand sends the welcome message
func handleStartCommand(chatID int64) {
	welcome := `🎬 *Video Downloader Bot*

Welcome! I can download videos from various platforms and send them directly to Telegram.

*Supported Platforms:*
• YouTube
• Facebook
• Twitter/X
• Instagram
• TikTok
• Reddit
• Twitch
• Vimeo
• Dailymotion

*How to Use:*
1. Send me a video URL
2. Select your preferred quality
3. Wait for the video to upload

*Commands:*
/start - Show this welcome message
/help - Show help information

Send a URL to get started!`

	msg := tgbotapi.NewMessage(chatID, welcome)
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Failed to send welcome message: %v", err)
	}
}

// handleHelpCommand sends the help message
func handleHelpCommand(chatID int64) {
	help := `📖 *Help & Usage*

*Supported Sites:*
Send me a URL from any of these platforms and I'll fetch the video for you:
• YouTube (youtube.com, youtu.be)
• Facebook Videos
• Twitter/X Posts
• Instagram Reels & Posts
• TikTok Videos
• Reddit Videos
• Twitch Clips
• Vimeo Videos
• Dailymotion

*Quality Selection:*
After sending a URL, you'll see quality options as inline buttons. Tap to select.

*Tips:*
• Some private or age-restricted videos may not work
• Higher quality = longer upload time
• Videos over 50MB may be rejected by Telegram

*Commands:*
/start - Welcome message
/help - This help message`

	msg := tgbotapi.NewMessage(chatID, help)
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Failed to send help message: %v", err)
	}
}

// handleVideoURL processes a video URL and shows quality options
func handleVideoURL(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	url := message.Text

	// Send processing message
	processingMsg := tgbotapi.NewMessage(chatID, "⏳ Processing... This may take a moment.")
	processingMsg, _ = bot.Send(processingMsg)

	// Extract video info using yt-dlp
	videoInfo, err := extractVideoInfo(url)
	if err != nil {
		log.Printf("Failed to extract video info: %v", err)
		editMsg := tgbotapi.NewEditMessageText(chatID, processingMsg.MessageID, formatErrorMessage(err))
		bot.EditMessageText(editMsg)
		return
	}

	// Store session
	session := userSessions.GetSession(userID)
	session.Mutex.Lock()
	session.VideoURL = url
	session.Title = videoInfo.Title
	session.Duration = formatDuration(videoInfo.Duration)
	session.Thumbnail = videoInfo.Thumbnail
	session.Formats = videoInfo.Formats
	session.Mutex.Unlock()

	// Create inline keyboard with quality options
	keyboard := createQualityKeyboard(videoInfo.Formats)

	// Format info message
	infoText := fmt.Sprintf(`📹 *%s*
⏱ Duration: %s
🎬 Uploader: %s

*Select quality:*`,
		escapeMarkdown(videoInfo.Title),
		session.Duration,
		escapeMarkdown(videoInfo.Uploader),
	)

	editMsg := tgbotapi.NewEditMessageText(chatID, processingMsg.MessageID, infoText)
	editMsg.ReplyMarkup = &keyboard
	editMsg.ParseMode = "Markdown"

	if _, err := bot.EditMessageText(editMsg); err != nil {
		log.Printf("Failed to edit message: %v", err)
	}
}

// handleCallback processes inline keyboard button presses
func handleCallback(callback *tgbotapi.CallbackQuery) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID
	data := callback.Data

	log.Printf("Callback from %s: %s", callback.From.UserName, data)

	// Acknowledge callback immediately
	bot.AnswerCallbackQuery(tgbotapi.NewCallback(callback.ID, ""))

	// Parse format selection
	if !strings.HasPrefix(data, "fmt_") {
		return
	}

	formatID := strings.TrimPrefix(data, "fmt_")
	session := userSessions.GetSession(userID)

	session.Mutex.RLock()
	title := session.Title
	duration := session.Duration
	thumbnail := session.Thumbnail
	var selectedFormat *VideoFormat
	for i := range session.Formats {
		if session.Formats[i].FormatID == formatID {
			selectedFormat = &session.Formats[i]
			break
		}
	}
	session.Mutex.RUnlock()

	if selectedFormat == nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Format not found. Please try again.")
		bot.Send(msg)
		return
	}

	// Send uploading message
	uploadingMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("📤 Uploading %s video...\n⏱ Duration: %s", selectedFormat.Quality, duration))
	uploadingMsg, _ = bot.Send(uploadingMsg)

	// Send video to Telegram
	err := sendVideo(chatID, selectedFormat, title, duration)
	if err != nil {
		log.Printf("Failed to send video: %v", err)
		editMsg := tgbotapi.NewEditMessageText(chatID, uploadingMsg.MessageID, formatUploadError(err))
		bot.EditMessageText(editMsg)
		return
	}

	// Update success message
	successMsg := fmt.Sprintf("✅ Video sent!\n📹 %s\n🎬 %s", escapeMarkdown(title), duration)
	editMsg := tgbotapi.NewEditMessageText(chatID, uploadingMsg.MessageID, successMsg)
	editMsg.ParseMode = "Markdown"
	bot.EditMessageText(editMsg)

	// Clear session
	userSessions.ClearSession(userID)
}

// extractVideoInfo runs yt-dlp and parses the JSON output
func extractVideoInfo(url string) (*YtDlpOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args := []string{
		"--dump-json",
		"--no-download",
		"--no-warnings",
		"--flat-playlist",
	}

	// Add cookies file if provided
	if cookiesFile != "" && fileExists(cookiesFile) {
		args = append(args, "--cookies", cookiesFile)
	}

	args = append(args, url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("processing timeout - video may be too long or site is slow")
		}
		return nil, fmt.Errorf("failed to run yt-dlp: %w", err)
	}

	var videoInfo YtDlpOutput
	if err := json.Unmarshal(output, &videoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp output: %w", err)
	}

	// Process and filter formats
	videoInfo.Formats = filterPlayableFormats(videoInfo.Formats)

	if len(videoInfo.Formats) == 0 {
		return nil, fmt.Errorf("no downloadable formats found")
	}

	return &videoInfo, nil
}

// filterPlayableFormats filters and sorts formats to only include playable ones
func filterPlayableFormats(formats []YtDlpFormat) []YtDlpFormat {
	var playable []YtDlpFormat
	seen := make(map[string]bool)

	for _, f := range formats {
		// Skip formats without URL
		if f.URL == "" {
			continue
		}

		// Skip pure audio formats
		if f.VCodec == "none" || f.VCodec == "" {
			continue
		}

		// Skip formats without video codec
		if f.Height == 0 && f.VCodec == "none" {
			continue
		}

		// Create unique key for deduplication
		key := fmt.Sprintf("%dp_%s", f.Height, f.Ext)
		if seen[key] && f.Height > 0 {
			continue
		}
		seen[key] = true

		// Build quality string
		quality := formatQuality(f)

		playable = append(playable, YtDlpFormat{
			FormatID:   f.FormatID,
			FormatNote: f.FormatNote,
			Ext:        f.Ext,
			Filesize:   f.Filesize,
			URL:        f.URL,
			Height:     f.Height,
			Width:      f.Width,
			TBR:        f.TBR,
			VCodec:     f.VCodec,
			ACodec:     f.ACodec,
		})
	}

	// Sort by height (prefer higher quality)
	for i := 0; i < len(playable)-1; i++ {
		for j := i + 1; j < len(playable); j++ {
			if playable[j].Height > playable[i].Height {
				playable[i], playable[j] = playable[j], playable[i]
			}
		}
	}

	// Limit to top 10 formats
	if len(playable) > 10 {
		playable = playable[:10]
	}

	return playable
}

// formatQuality creates a human-readable quality string
func formatQuality(f YtDlpFormat) string {
	if f.Height > 0 {
		return fmt.Sprintf("%dp", f.Height)
	}
	if f.Width > 0 {
		return fmt.Sprintf("%dw", f.Width)
	}
	if f.FormatNote != "" {
		return f.FormatNote
	}
	return f.Ext
}

// createQualityKeyboard creates inline keyboard buttons for quality selection
func createQualityKeyboard(formats []YtDlpFormat) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, format := range formats {
		quality := formatQuality(YtDlpFormat{
			Height:     format.Height,
			Width:      format.Width,
			FormatNote: format.FormatNote,
			Ext:        format.Ext,
		})

		// Format size string
		sizeStr := ""
		if format.Filesize > 0 {
			sizeStr = fmt.Sprintf(" (~%s)", formatFilesize(format.Filesize))
		}

		// Build button text
		btnText := fmt.Sprintf("%s - %s%s", quality, strings.ToUpper(format.Ext), sizeStr)

		button := tgbotapi.NewInlineKeyboardButtonData(btnText, "fmt_"+format.FormatID)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// sendVideo sends a video to Telegram using the direct URL
func sendVideo(chatID int64, format *VideoFormat, title, duration string) error {
	caption := fmt.Sprintf("📹 %s\n⏱ %s", title, duration)

	msg := tgbotapi.NewVideo(chatID, tgbotapi.FileURL(format.URL))
	msg.Caption = caption
	msg.ParseMode = "Markdown"
	msg.SupportsStreaming = true

	_, err := bot.Send(msg)
	return err
}

// Helper functions

func isSupportedURL(url string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}

	for _, pattern := range supportedURLPatterns {
		if pattern.MatchString(url) {
			return true
		}
	}
	return false
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "Unknown"
	}
	d := int(seconds)
	h := d / 3600
	m := (d % 3600) / 60
	s := d % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatFilesize(bytes int64) string {
	if bytes <= 0 {
		return "Unknown"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatErrorMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "private"):
		return "🔒 This video is private or requires login."
	case strings.Contains(msg, "not found") || strings.Contains(msg, "404"):
		return "❌ Video not found. It may have been deleted."
	case strings.Contains(msg, "timeout"):
		return "⏱ Processing timeout. The video might be too long or the site is slow. Please try again."
	case strings.Contains(msg, "unsupported") || strings.Contains(msg, "format"):
		return "❌ Unsupported video format."
	case strings.Contains(msg, "no downloadable"):
		return "📁 No downloadable formats found for this URL."
	default:
		return fmt.Sprintf("❌ Error: %s\n\nPlease try again or send a different URL.", msg)
	}
}

func formatUploadError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Too many requests"):
		return "⚠️ Rate limit exceeded. Please wait a moment and try again."
	case strings.Contains(msg, "file too large"):
		return "⚠️ This video is too large for Telegram (>50MB)."
	default:
		return fmt.Sprintf("❌ Upload failed: %s", msg)
	}
}

func escapeMarkdown(text string) string {
	// Escape special characters for Telegram Markdown
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"`", "\\`",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// startHealthServer starts the health check HTTP server
func startHealthServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Health server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("Health server error: %v", err)
	}
}

// VideoFormat conversion for session storage
type VideoFormat2 struct {
	FormatID   string
	Quality    string
	Ext        string
	Filesize   int64
	URL        string
	Note       string
}

// ConvertYtDlpFormat converts YtDlpFormat to VideoFormat
func ConvertYtDlpFormat(f YtDlpFormat) VideoFormat {
	return VideoFormat{
		FormatID: f.FormatID,
		Quality:  formatQuality(f),
		Ext:      f.Ext,
		Filesize: f.Filesize,
		URL:      f.URL,
		Note:     f.FormatNote,
	}
}
