package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/pborman/getopt/v2"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

var verbose bool

func mustGetEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		log.Fatal("Environmental variable must be set:", key)
	}
	return value
}

func main() {
	getopt.FlagLong(&verbose, "verbose", 'v', "verbose output")
	helpFlag := getopt.BoolLong("help", 'h', "show help")
	getopt.SetParameters("message")
	getopt.Parse()
	if *helpFlag {
		getopt.Usage()
		os.Exit(1)
	}
	webAPI := slack.New(
		mustGetEnv("SLACK_BOT_TOKEN"),
		slack.OptionAppLevelToken(mustGetEnv("SLACK_APP_TOKEN")),
		slack.OptionDebug(verbose),
		slack.OptionLog(log.New(os.Stderr, "api: ", log.Lshortfile|log.LstdFlags)),
	)
	socketMode := socketmode.New(
		webAPI,
		socketmode.OptionDebug(verbose),
		socketmode.OptionLog(log.New(os.Stdout, "sm: ", log.Lshortfile|log.LstdFlags)),
	)
	authTest, authTestErr := webAPI.AuthTest()
	if authTestErr != nil {
		fmt.Fprintf(os.Stderr, "SLACK_BOT_TOKEN is invalid: %v\n", authTestErr)
		os.Exit(1)
	}
	selfUserID := authTest.UserID

	go func() {
		for envelope := range socketMode.Events {
			switch envelope.Type {
			case socketmode.EventTypeEventsAPI:
				// イベント API のハンドリング

				// 3 秒以内にとりあえず ack
				socketMode.Ack(*envelope.Request)

				eventPayload, _ := envelope.Data.(slackevents.EventsAPIEvent)
				switch eventPayload.Type {
				case slackevents.CallbackEvent:
					switch event := eventPayload.InnerEvent.Data.(type) {
					case *slackevents.MessageEvent:
						if event.User != selfUserID && strings.Contains(event.Text, "こんにちは") {
							_, _, err := webAPI.PostMessage(
								event.Channel,
								slack.MsgOptionText(
									fmt.Sprintf(":wave: こんにちは <@%v> さん！", event.User),
									false,
								),
							)
							if err != nil {
								log.Printf("Failed to reply: %v", err)
							}
						}
					default:
						socketMode.Debugf("Skipped: %v", event)
					}
				default:
					socketMode.Debugf("unsupported Events API eventPayload received")
				}
			case socketmode.EventTypeInteractive:
				// ショートカットのハンドリングとモーダル起動
				payload, _ := envelope.Data.(slack.InteractionCallback)
				switch payload.Type {
				case slack.InteractionTypeShortcut:
					if payload.CallbackID == "socket-mode-shortcut" {
						socketMode.Ack(*envelope.Request)
						modalView := slack.ModalViewRequest{
							Type:       "modal",
							CallbackID: "modal-id",
							Title: slack.NewTextBlockObject(
								"plain_text",
								"タスク登録",
								false,
								false,
							),
							Submit: slack.NewTextBlockObject(
								"plain_text",
								"送信",
								false,
								false,
							),
							Close: slack.NewTextBlockObject(
								"plain_text",
								"キャンセル",
								false,
								false,
							),
							Blocks: slack.Blocks{
								BlockSet: []slack.Block{
									slack.NewInputBlock(
										"input-task",
										slack.NewTextBlockObject(
											"plain_text",
											"タスク",
											false,
											false,
										),
										// multiline is not yet supported
										slack.NewPlainTextInputBlockElement(
											slack.NewTextBlockObject(
												"plain_text",
												"タスクの詳細・期限などを書いてください",
												false,
												false,
											),
											"input",
										),
									),
								},
							},
						}
						resp, err := webAPI.OpenView(payload.TriggerID, modalView)
						if err != nil {
							log.Printf("Failed to opemn a modal: %v", err)
						}
						socketMode.Debugf("views.open response: %v", resp)
					}
				case slack.InteractionTypeViewSubmission:
					// モーダルからの送信をハンドリング
					if payload.CallbackID == "modal-id" {
						socketMode.Debugf("Submitted Data: %v", payload.View.State.Values)
						socketMode.Ack(*envelope.Request)
					}
				default:
					socketMode.Debugf("Skipped: %v", payload)
				}

			default:
				socketMode.Debugf("Skipped: %v", envelope.Type)
			}
		}
	}()

	err := socketMode.Run()
	if err != nil {
		log.Fatal(err)
	}
}
