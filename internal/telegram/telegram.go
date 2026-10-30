package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	botToken string
	chatID   string
	client   *http.Client
}

func NewClient(botToken, chatID string) *Client {
	return &Client{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// GenerateOTP generates a random 6-digit numeric OTP
func GenerateOTP() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	n := (int(b[0])<<16 | int(b[1])<<8 | int(b[2])) % 1000000
	return fmt.Sprintf("%06d", n)
}

// SendPhotoWithOTP sends the intruder image and the unlock OTP to the owner's Telegram
func (c *Client) SendPhotoWithOTP(otp string, imageBase64 string) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("telegram token or chat ID is not configured")
	}

	// Clean base64 header if present (e.g. data:image/jpeg;base64,...)
	if idx := strings.Index(imageBase64, ","); idx != -1 {
		imageBase64 = imageBase64[idx+1:]
	}

	imageBytes, err := base64.StdEncoding.DecodeString(imageBase64)
	if err != nil {
		return fmt.Errorf("failed to decode base64 image: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", c.botToken)
	caption := fmt.Sprintf("🚨 <b>PENYUSUP TERDETEKSI DI LAPTOP!</b>\n\n🔑 <b>KODE OTP PEMBUKA:</b> <code>%s</code>\n\nMasukkan 6 digit angka di atas pada laptop untuk membuka kuncian.", otp)

	return c.sendMediaFile(url, "photo", "intruder.jpg", "image/jpeg", imageBytes, caption)
}

// SendVideo sends the surveillance video clip to Telegram
func (c *Client) SendVideo(videoBase64 string, caption string) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("telegram token or chat ID is not configured")
	}

	if idx := strings.Index(videoBase64, ","); idx != -1 {
		videoBase64 = videoBase64[idx+1:]
	}

	videoBytes, err := base64.StdEncoding.DecodeString(videoBase64)
	if err != nil {
		return fmt.Errorf("failed to decode base64 video: %w", err)
	}

	if caption == "" {
		caption = "📹 <b>REKAMAN BUKTI PENYUSUP (VIDEO + AUDIO)</b>\nSistem merekam sebelum alarm berbunyi."
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendVideo", c.botToken)
	return c.sendMediaFile(url, "video", "surveillance.webm", "video/webm", videoBytes, caption)
}

func (c *Client) sendMediaFile(url, fieldName, filename, mimeType string, data []byte, caption string) error {
	var reqBody bytes.Buffer
	writer := multipart.NewWriter(&reqBody)

	_ = writer.WriteField("chat_id", c.chatID)
	_ = writer.WriteField("caption", caption)
	_ = writer.WriteField("parse_mode", "HTML")

	if endpoint := fieldName; endpoint == "video" {
		_ = writer.WriteField("supports_streaming", "true")
	}

	replyMarkup := `{"inline_keyboard":[[{"text":"🔓 SAYA SENDIRI (DISARM)","callback_data":"disarm"}],[{"text":"🚨 KUNCI PERMANEN (NUKE)","callback_data":"nuke"}]]}`
	_ = writer.WriteField("reply_markup", replyMarkup)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
	h.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, &reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram api %s returned status %d: %s", fieldName, resp.StatusCode, string(respBytes))
	}

	return nil
}

// SendMessageWithInlineButton sends a message with an interactive inline button
func (c *Client) SendMessageWithInlineButton(text, buttonText, callbackData string) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("bot token or chat ID not set")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)
	replyMarkup := fmt.Sprintf(`{"inline_keyboard":[[{"text":"%s","callback_data":"%s"}]]}`, buttonText, callbackData)

	payload := map[string]string{
		"chat_id":      c.chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": replyMarkup,
	}

	jsonBytes, _ := json.Marshal(payload)
	resp, err := c.client.Post(apiURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SendAlertMessage sends a text-only alert to Telegram
func (c *Client) SendAlertMessage(message string) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("bot token or chat ID not set")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)
	payload := map[string]string{
		"chat_id":    c.chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	jsonBytes, _ := json.Marshal(payload)
	resp, err := c.client.Post(apiURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SendMessageWithURLButton sends an inline button linking to an external viewer URL
func (c *Client) SendMessageWithURLButton(text, buttonText, targetURL string) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("bot token or chat ID not set")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)
	replyMarkup := fmt.Sprintf(`{"inline_keyboard":[[{"text":"%s","url":"%s"}]]}`, buttonText, targetURL)

	payload := map[string]string{
		"chat_id":      c.chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": replyMarkup,
	}

	jsonBytes, _ := json.Marshal(payload)
	resp, err := c.client.Post(apiURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// DownloadFile downloads a file from Telegram by its file_id
func (c *Client) DownloadFile(fileID string) ([]byte, string, error) {
	if c.botToken == "" {
		return nil, "", fmt.Errorf("bot token not set")
	}

	getFileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", c.botToken, fileID)
	resp, err := c.client.Get(getFileURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	var res struct {
		Ok     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || !res.Ok {
		return nil, "", fmt.Errorf("failed to get file path from telegram")
	}

	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.botToken, res.Result.FilePath)
	dlResp, err := c.client.Get(downloadURL)
	if err != nil {
		return nil, "", err
	}
	defer dlResp.Body.Close()

	data, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return nil, "", err
	}

	return data, res.Result.FilePath, nil
}

// AnswerCallback acknowledges a Telegram callback query instantly
func (c *Client) AnswerCallback(callbackID, text string) {
	if c.botToken == "" || callbackID == "" {
		return
	}
	go func() {
		answerURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery?callback_query_id=%s&text=%s", c.botToken, callbackID, url.QueryEscape(text))
		resp, err := c.client.Get(answerURL)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}()
}

// StartPolling listens for remote commands, inline button taps, and voice broadcast messages with real-time HTTP long polling
func (c *Client) StartPolling(ctx context.Context, onNuke func(), onUnlock func(), onAudioBroadcast func(audioBase64 string, mimeType string), onArm func(), onStatus func() string, onGetStreamURL func() string) {
	if c.botToken == "" {
		return
	}

	// 1. Initial backlog flush: skip stale messages so startup doesn't replay ancient history
	offset := 0
	initURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=-1&timeout=0", c.botToken)
	if resp, err := c.client.Get(initURL); err == nil {
		var initRes struct {
			Ok     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
			} `json:"result"`
		}
		if json.NewDecoder(resp.Body).Decode(&initRes) == nil && initRes.Ok && len(initRes.Result) > 0 {
			offset = initRes.Result[len(initRes.Result)-1].UpdateID + 1
		}
		_ = resp.Body.Close()
	}

	// 2. High-performance long-polling loop (timeout=15s on server, 0s client sleep)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			updateURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=15", c.botToken, offset)
			resp, err := c.client.Get(updateURL)
			if err != nil {
				// Backoff slightly on network disconnect
				time.Sleep(1 * time.Second)
				continue
			}

			var result struct {
				Ok     bool `json:"ok"`
				Result []struct {
					UpdateID int `json:"update_id"`
					Message  *struct {
						Text string `json:"text"`
						Chat struct {
							ID int64 `json:"id"`
						} `json:"chat"`
						Voice *struct {
							FileID   string `json:"file_id"`
							MimeType string `json:"mime_type"`
						} `json:"voice"`
						Audio *struct {
							FileID   string `json:"file_id"`
							MimeType string `json:"mime_type"`
						} `json:"audio"`
					} `json:"message"`
					CallbackQuery *struct {
						ID   string `json:"id"`
						Data string `json:"data"`
					} `json:"callback_query"`
				} `json:"result"`
			}

			decodeErr := json.NewDecoder(resp.Body).Decode(&result)
			_ = resp.Body.Close()

			if decodeErr != nil || !result.Ok {
				time.Sleep(1 * time.Second)
				continue
			}

			for _, u := range result.Result {
				offset = u.UpdateID + 1

				// 1. Handle Inline Button Clicks (Instant ACK < 50ms)
				if u.CallbackQuery != nil {
					cb := u.CallbackQuery
					if cb.Data == "disarm" || cb.Data == "unlock" {
						c.AnswerCallback(cb.ID, "✅ SnoopTrap Disarmed!")
						go func() {
							_ = c.SendMessageWithInlineButton("✅ <b>SnoopTrap Disarmed!</b>\nSelamat bekerja kembali, sistem penjagaan dinonaktifkan.\n\nJika mau pergi lagi, tap tombol di bawah:", "🛡️ AKTIFKAN SIAGA SEKARANG (/arm)", "arm")
							if onUnlock != nil {
								onUnlock()
							}
						}()
					} else if cb.Data == "arm" || cb.Data == "siaga" {
						c.AnswerCallback(cb.ID, "🛡️ Siaga Aktif!")
						go func() {
							_ = c.SendMessageWithInlineButton("🛡️ <b>SnoopTrap Siaga Aktif!</b>\nLaptop sedang dijaga. Sentuhan mouse/keyboard akan memicu jebakan!\n\nSaat mau kembali ke meja, tap tombol di bawah:", "🔓 SAYA SUDAH KEMBALI (DISARM)", "disarm")
							if onArm != nil {
								onArm()
							}
						}()
					} else if cb.Data == "nuke" || cb.Data == "alarm" {
						c.AnswerCallback(cb.ID, "🚨 Mengunci Workstation!")
						if onNuke != nil {
							go onNuke()
						}
					}
				}

				// 2. Handle Voice Notes (VN) & Audio Messages (Megaphone Broadcast)
				if u.Message != nil {
					var fileID string
					var mimeType string

					if u.Message.Voice != nil {
						fileID = u.Message.Voice.FileID
						mimeType = u.Message.Voice.MimeType
						if mimeType == "" {
							mimeType = "audio/ogg"
						}
					} else if u.Message.Audio != nil {
						fileID = u.Message.Audio.FileID
						mimeType = u.Message.Audio.MimeType
						if mimeType == "" {
							mimeType = "audio/mpeg"
						}
					}

					if fileID != "" && onAudioBroadcast != nil {
						go func(fid, mime string) {
							audioBytes, _, err := c.DownloadFile(fid)
							if err == nil && len(audioBytes) > 0 {
								base64Data := base64.StdEncoding.EncodeToString(audioBytes)
								onAudioBroadcast(base64Data, mime)
								_ = c.SendAlertMessage("📢 <b>Megaphone Broadcast Aktif!</b>\nPesan suara / VN Anda sedang diputar dengan volume maksimal di speaker laptop!")
							}
						}(fileID, mimeType)
					}

					// 3. Handle Text Commands (/arm, /disarm, /nuke, /status, /stream, /help)
					txt := strings.ToLower(strings.TrimSpace(u.Message.Text))
					if (txt == "/arm" || txt == "/siaga" || txt == "/jaga" || txt == "arm" || txt == "siaga") && onArm != nil {
						go func() {
							_ = c.SendMessageWithInlineButton("🛡️ <b>SnoopTrap Siaga Aktif!</b>\nLaptop Anda sedang dijaga penuh. Jika mouse atau keyboard disentuh, jebakan kamera & alarm langsung terpicu!\n\nSaat Anda mau kembali ke meja, tap tombol di bawah:", "🔓 SAYA SUDAH KEMBALI (DISARM)", "disarm")
							onArm()
						}()
					} else if (txt == "/unlock" || txt == "/disarm" || txt == "disarm" || txt == "unlock") && onUnlock != nil {
						go func() {
							_ = c.SendMessageWithInlineButton("✅ <b>SnoopTrap Disarmed!</b>\nSelamat bekerja kembali, sistem penjagaan dinonaktifkan.\n\nJika mau pergi lagi, tap tombol di bawah:", "🛡️ AKTIFKAN SIAGA SEKARANG (/arm)", "arm")
							onUnlock()
						}()
					} else if (txt == "/stream" || txt == "/live") && onGetStreamURL != nil {
						go func() {
							streamURL := onGetStreamURL()
							if streamURL != "" {
								_ = c.SendMessageWithURLButton("🔴 <b>LIVE SURVEILLANCE FEED</b>\n\nTap tombol di bawah untuk menonton siaran langsung kamera & audio laptop secara real-time:", "👁️ TONTON LIVE STREAM", streamURL)
							} else {
								_ = c.SendAlertMessage("⚠️ Menyiapkan feed live stream... Coba kirim /stream lagi dalam 2 detik.")
							}
						}()
					} else if txt == "/nuke" && onNuke != nil {
						go func() {
							_ = c.SendAlertMessage("🚨 <b>Perintah Nuke Diterima!</b> Mengunci workstation dan menutup aplikasi.")
							onNuke()
						}()
					} else if txt == "/status" && onStatus != nil {
						go func() {
							currentStatus := onStatus()
							_ = c.SendAlertMessage(fmt.Sprintf("📊 <b>Status SnoopTrap:</b>\n\n%s", currentStatus))
						}()
					} else if txt == "/start" || txt == "/help" {
						go func() {
							helpMsg := `🤖 <b>SnoopTrap Remote Guardian</b>

Perintah yang tersedia:
🛡️ <b>/arm</b> : Aktifkan mode siaga sekarang
🔓 <b>/disarm</b> : Matikan siaga / buka kuncian
🔴 <b>/stream</b> : Tonton siaran langsung kamera & mic real-time
🎙️ <b>Kirim Voice Note (VN)</b> : Diputar langsung di speaker laptop (volume 100%)
🚨 <b>/nuke</b> : Kunci Windows darurat
📊 <b>/status</b> : Cek status laptop saat ini`
							_ = c.SendMessageWithInlineButton(helpMsg, "🛡️ AKTIFKAN SIAGA SEKARANG", "arm")
						}()
					}
				}
			}
		}
	}
}
