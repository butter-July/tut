package main

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"

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
	Base http.RoundTripper
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(context.Background())
	return t.Base.RoundTrip(req)
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	var (
		botToken     = os.Getenv("TELEGRAM_BOT_TOKEN")
		vercelApiKey = os.Getenv("AI_GATEWAY_API_KEY")
	)

	clientCfg := openai.DefaultConfig(vercelApiKey)
	clientCfg.BaseURL = "https://ai-gateway.vercel.sh/v1"
	aiClient = openai.NewClientWithConfig(clientCfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	tut, err := bot.New(botToken, bot.WithDefaultHandler(handler))
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	log.Println("Bot started...")
	tut.Start(ctx)
}

func toBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func handler(ctx context.Context, b *bot.Bot, update *models.Update) {

	chatID := update.Message.Chat.ID

	val, _ := messages.LoadOrStore(chatID, []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "你是Tut，一个精通编程的AI,你可以回答好多好多问题,不止编程相关的，回答字数不超过50,在用户发送delete时你会说明历史消息已经删除",
		},
	})
	history := val.([]openai.ChatCompletionMessage)

	var userContent string
	if update.Message.Photo != nil {

		photo := update.Message.Photo[len(update.Message.Photo)-1]

		fileID := photo.FileID

		file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
		if err != nil {
			log.Fatalln(err)
		}
		downloadURL := b.FileDownloadLink(file)
		resp, err := http.Get(downloadURL)
		if err != nil {
			log.Fatalln(err)
		}

		defer resp.Body.Close()

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalln(err)
		}
		var base64Encoding string
		contentType := http.DetectContentType(bytes)
		switch contentType {
		case "image/jpeg":
			base64Encoding += "data:image/jpeg;base64,"
		case "image/png":
			base64Encoding += "data:image/png;base64,"
		default:
			log.Fatalln("contentType unsupported: " + contentType)
		}

		base64Encoding += toBase64(bytes)

		userContent = base64Encoding
	} else if update.Message.Text != "" {
		userContent = update.Message.Text
	}

	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userContent,
	})

	req := openai.ChatCompletionRequest{
		Model:    "openai/gpt-5-nano",
		Messages: history,
	}

	resp, err := aiClient.CreateChatCompletion(ctx, req)
	if err != nil {
		log.Fatalln(err)
	}

	if len(resp.Choices) > 0 {
		answer := resp.Choices[0].Message.Content
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   answer,
		})
		if err != nil {
			log.Fatalln(err)
		}

		history = append(history, resp.Choices[0].Message)
		messages.Store(chatID, history)
	}
	if update.Message.Text == "delete" {
		messages.Delete(chatID)
	}
}
