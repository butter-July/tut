package main

import (
	"context"
	"encoding/base64"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
)

var (
	aiClient *openai.Client
	messages sync.Map
)

type AuthTransport struct {
	Base   http.RoundTripper
	APIKey string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(context.Background())
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	return t.Base.RoundTrip(req)
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	proxyurl := os.Getenv("PROXY_URL")
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	api := os.Getenv("AI_GATEWAY_API_KEY")
	proxyURL, _ := url.Parse(proxyurl)
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	authTransport := &AuthTransport{
		Base:   transport,
		APIKey: api,
	}

	httpClient := &http.Client{
		Transport: authTransport,
		Timeout:   60 * time.Second,
	}

	aiClient = openai.NewClientWithConfig(openai.ClientConfig{
		BaseURL:    "https://ai-gateway.vercel.sh/v1",
		HTTPClient: httpClient,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	b, err := bot.New(botToken, bot.WithDefaultHandler(handler))
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	log.Println("Bot started...")
	b.Start(ctx)
}
func toBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func handler(ctx context.Context, b *bot.Bot, update *models.Update) {

	chatID := update.Message.Chat.ID

	val, _ := messages.LoadOrStore(chatID, []openai.ChatCompletionMessage{
		{
			Role:    "system",
			Content: "你是Tut，一个精通编程的AI,你可以回答好多好多问题,不止编程相关的，回答字数不超过50",
		},
	})
	history := val.([]openai.ChatCompletionMessage)
	
	var userContent string
	if update.Message.Photo != nil {

		photo := update.Message.Photo[len(update.Message.Photo)-1]

		fileID := photo.FileID

		file, _ := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
		download := b.FileDownloadLink(file)
		resp, _ := http.Get(download)

		defer resp.Body.Close()

		bytes, _ := ioutil.ReadAll(resp.Body)
		var base64Encoding string
		contentType := http.DetectContentType(bytes)
		switch contentType {
		case "image/jpeg":
			base64Encoding += "data:image/jpeg;base64,"
		case "image/png":
			base64Encoding += "data:image/png;base64,"
		}
		base64Encoding += toBase64(bytes)

		userContent = base64Encoding
	} else if update.Message.Text != "" {
		userContent = update.Message.Text
	}
	history = append(history, openai.ChatCompletionMessage{
		Role:    "user",
		Content: userContent,
	})

	req := openai.ChatCompletionRequest{
		Model:    "openai/gpt-5-nano",
		Messages: history,
	}

	resp, _ := aiClient.CreateChatCompletion(ctx, req)

	if len(resp.Choices) > 0 {
		answer := resp.Choices[0].Message.Content
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   answer,
		})

		history = append(history, resp.Choices[0].Message)
		messages.Store(chatID, history)
	}
}
