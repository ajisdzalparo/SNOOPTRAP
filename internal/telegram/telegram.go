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
			Timeout: 15 * time.Second,
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

	var reqBody bytes.Buffer
	writer := multipart.NewWriter(&reqBody)

	_ = writer.WriteField("chat_id", c.chatID)
	caption := fmt.Sprintf("⚠️ ALERT: Ada yang menyentuh laptop lu!\n\n🔑 Masukkan OTP ini di layar untuk unlock: <code>%s</code>\n🕒 Waktu: %s",
		otp, time.Now().Format("2006-01-02 15:04:05"))
	_ = writer.WriteField("caption", caption)
	_ = writer.WriteField("parse_mode", "HTML")

	replyMarkup := `{"inline_keyboard":[[{"text":"🔓 SAYA SENDIRI (DISARM)","callback_data":"disarm"}],[{"text":"🚨 KUNCI PERMANEN (NUKE)","callback_data":"nuke"}]]}`
	_ = writer.WriteField("reply_markup", replyMarkup)

	part, err := writer.CreateFormFile("photo", "intruder.jpg")
	if err != nil {
		return err
	}
	if _, err := part.Write(imageBytes); err != nil {
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
		return fmt.Errorf("telegram api returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	return nil
}

// SendVideo sends a video recording with audio to the owner's Telegram (with automatic fallback to sendDocument)
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
		caption = fmt.Sprintf("🎥 <b>INTRUDER EVIDENCE RECORDING</b>\nBukti rekaman video & audio penyusup.\n🕒 Waktu: %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	// 1. Attempt sendVideo (inline video player in Telegram)
	err = c.dispatchVideoPayload("sendVideo", "video", "intruder_evidence.mp4", videoBytes, caption)
	if err == nil {
		return nil
	}

	// 2. Fallback to sendDocument if sendVideo is rejected
	return c.dispatchVideoPayload("sendDocument", "document", "intruder_evidence.mp4", videoBytes, caption)
}

func (c *Client) dispatchVideoPayload(endpoint, fieldName, filename string, data []byte, caption string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.botToken, endpoint)

	var reqBody bytes.Buffer
	writer := multipart.NewWriter(&reqBody)

	_ = writer.WriteField("chat_id", c.chatID)
	_ = writer.WriteField("caption", caption)
	_ = writer.WriteField("parse_mode", "HTML")

	if endpoint == "sendVideo" {
		_ = writer.WriteField("supports_streaming", "true")
	}

	replyMarkup := `{"inline_keyboard":[[{"text":"🔓 SAYA SENDIRI (DISARM)","callback_data":"disarm"}],[{"text":"🚨 KUNCI PERMANEN (NUKE)","callback_data":"nuke"}]]}`
	_ = writer.WriteField("reply_markup", replyMarkup)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
	h.Set("Content-Type", "video/mp4")
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
		return fmt.Errorf("telegram api %s returned status %d: %s", endpoint, resp.StatusCode, string(respBytes))
	}

	return nil
}

// StartPolling listens for remote commands and inline button taps
func (c *Client) StartPolling(ctx context.Context, onNuke func(), onUnlock func()) {
	if c.botToken == "" {
		return
	}

	offset := 0
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=2", c.botToken, offset)
			resp, err := c.client.Get(url)
			if err != nil {
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
					} `json:"message"`
					CallbackQuery *struct {
						ID   string `json:"id"`
						Data string `json:"data"`
					} `json:"callback_query"`
				} `json:"result"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Ok {
				for _, u := range result.Result {
					offset = u.UpdateID + 1

					// 1. Handle Inline Button Clicks
					if u.CallbackQuery != nil {
						cb := u.CallbackQuery
						answerURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery?callback_query_id=%s&text=Perintah+Diterima", c.botToken, cb.ID)
						_, _ = c.client.Get(answerURL)

						if (cb.Data == "disarm" || cb.Data == "unlock") && onUnlock != nil {
							onUnlock()
						} else if (cb.Data == "nuke" || cb.Data == "alarm") && onNuke != nil {
							onNuke()
						}
					}

					// 2. Handle Text Commands (/unlock, /disarm, /nuke)
					if u.Message != nil {
						txt := strings.ToLower(strings.TrimSpace(u.Message.Text))
						if txt == "/nuke" && onNuke != nil {
							onNuke()
						} else if (txt == "/unlock" || txt == "/disarm") && onUnlock != nil {
							onUnlock()
						}
					}
				}
			}
			resp.Body.Close()
		}
	}
}

