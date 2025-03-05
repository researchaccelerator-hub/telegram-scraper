package crawl

import (
	"github.com/researchaccelerator-hub/telegram-scraper/common"
	"github.com/researchaccelerator-hub/telegram-scraper/crawler"
	"github.com/researchaccelerator-hub/telegram-scraper/state"
	"github.com/researchaccelerator-hub/telegram-scraper/telegramhelper"
	"github.com/rs/zerolog/log"
	"github.com/zelenin/go-tdlib/client"
)

// Run connects to a Telegram channel and crawls its messages.
func Run(crawlID, channelUsername, storagePrefix string, sm state.StateManager, cfg common.CrawlerConfig) error {
	// Initialize Telegram client
	service := &telegramhelper.RealTelegramService{}
	tdlibClient, err := service.InitializeClient(storagePrefix)
	if err != nil {
		log.Error().Err(err).Stack().Msg("Failed to initialize Telegram client")
		return err
	}

	// Ensure tdlibClient is closed after the function finishes
	defer closeClient(tdlibClient)

	// Get channel information
	channelInfo, err := getChannelInfo(tdlibClient, channelUsername)
	if err != nil {
		return err
	}

	// Process all messages in the channel
	err = processAllMessages(tdlibClient, channelInfo, crawlID, channelUsername, sm, cfg)
	if err != nil {
		return err
	}

	return nil
}

// closeClient safely closes the Telegram client
func closeClient(tdlibClient crawler.TDLibClient) {
	if tdlibClient != nil {
		log.Debug().Msg("Closing tdlibClient...")
		_, err := tdlibClient.Close()
		if err != nil {
			log.Error().Err(err).Msg("Error closing tdlibClient")
		} else {
			log.Info().Msg("tdlibClient closed successfully")
		}
	}
}

// channelInfo holds all necessary channel data
type channelInfo struct {
	chat           *client.Chat
	chatDetails    *client.Chat
	supergroup     *client.Supergroup
	supergroupInfo *client.SupergroupFullInfo
	totalViews     int32
	messageCount   int32
}

// TotalViewsGetter is a function type for retrieving total view count
type TotalViewsGetter func(client crawler.TDLibClient, chatID int64, channelUsername string) (int, error)

// MessageCountGetter is a function type for retrieving message count
type MessageCountGetter func(client crawler.TDLibClient, chatID int64, channelUsername string) (int, error)

func getChannelInfo(tdlibClient crawler.TDLibClient, channelUsername string) (*channelInfo, error) {
	return getChannelInfoWithDeps(
		tdlibClient,
		channelUsername,
		telegramhelper.GetTotalChannelViews,
		telegramhelper.GetMessageCount,
	)
}

// getChannelInfoWithDeps is the dependency-injected version of getChannelInfo
func getChannelInfoWithDeps(
	tdlibClient crawler.TDLibClient,
	channelUsername string,
	getTotalViewsFn TotalViewsGetter,
	getMessageCountFn MessageCountGetter,
) (*channelInfo, error) {
	// Search for the channel
	chat, err := tdlibClient.SearchPublicChat(&client.SearchPublicChatRequest{
		Username: channelUsername,
	})
	if err != nil {
		log.Error().Err(err).Stack().Msgf("Failed to find channel: %v", channelUsername)
		return nil, err
	}

	chatDetails, err := tdlibClient.GetChat(&client.GetChatRequest{
		ChatId: chat.Id,
	})
	if err != nil {
		log.Error().Err(err).Stack().Msgf("Failed to get chat details for: %v", channelUsername)
		return nil, err
	}

	// Get channel stats
	totalViews := 0
	if getTotalViewsFn != nil {
		totalViewsVal, err := getTotalViewsFn(tdlibClient, chat.Id, channelUsername)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to get total views for channel: %v", channelUsername)
			// Continue anyway, this is not critical
		} else {
			totalViews = totalViewsVal
		}
	}

	messageCount := 0
	if getMessageCountFn != nil {
		messageCountVal, err := getMessageCountFn(tdlibClient, chat.Id, channelUsername)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to get message count for channel: %v", channelUsername)
			// Continue anyway, this is not critical
		} else {
			messageCount = messageCountVal
		}
	}

	// Get supergroup information if available
	var supergroup *client.Supergroup
	var supergroupInfo *client.SupergroupFullInfo

	if chat.Type != nil {
		if supergroupType, ok := chat.Type.(*client.ChatTypeSupergroup); ok {
			supergroup, err = tdlibClient.GetSupergroup(&client.GetSupergroupRequest{
				SupergroupId: supergroupType.SupergroupId,
			})
			if err != nil {
				log.Warn().Err(err).Msgf("Failed to get supergroup info for: %v", channelUsername)
				// Continue anyway, this is not critical
			}

			if supergroup != nil {
				req := &client.GetSupergroupFullInfoRequest{
					SupergroupId: supergroup.Id,
				}
				supergroupInfo, err = tdlibClient.GetSupergroupFullInfo(req)
				if err != nil {
					log.Warn().Err(err).Msgf("Failed to get supergroup full info for: %v", channelUsername)
					// Continue anyway, this is not critical
				}
			}
		}
	}

	return &channelInfo{
		chat:           chat,
		chatDetails:    chatDetails,
		supergroup:     supergroup,
		supergroupInfo: supergroupInfo,
		totalViews:     int32(totalViews),
		messageCount:   int32(messageCount),
	}, nil
}

type MessageFetcher interface {
	FetchMessages(tdlibClient crawler.TDLibClient, chatID int64, fromMessageID int64) ([]*client.Message, error)
}

type DefaultMessageFetcher struct{}

func (f *DefaultMessageFetcher) FetchMessages(tdlibClient crawler.TDLibClient, chatID int64, fromMessageID int64) ([]*client.Message, error) {
	// Call the original fetchMessages implementation
	chatHistory, err := tdlibClient.GetChatHistory(&client.GetChatHistoryRequest{
		ChatId:        chatID,
		Limit:         100,
		FromMessageId: fromMessageID,
	})

	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get chat history")
		return nil, err
	}

	return chatHistory.Messages, nil
}

type MessageProcessor interface {
	// ProcessMessage processes a single Telegram message.
	ProcessMessage(tdlibClient crawler.TDLibClient, message *client.Message, info *channelInfo, crawlID string, channelUsername string, sm *state.StateManager, cfg common.CrawlerConfig) error
}

// DefaultMessageProcessor implements the MessageProcessor interface using the default processMessage function
type DefaultMessageProcessor struct{}

// ProcessMessage implements the MessageProcessor interface
func (p *DefaultMessageProcessor) ProcessMessage(tdlibClient crawler.TDLibClient, message *client.Message, info *channelInfo, crawlID string, channelUsername string, sm *state.StateManager, cfg common.CrawlerConfig) error {
	return processMessage(tdlibClient, message, info, crawlID, channelUsername, *sm, cfg)
}

// processAllMessages retrieves and processes all messages from a channel
func processAllMessages(tdlibClient crawler.TDLibClient, info *channelInfo, crawlID, channelUsername string, sm state.StateManager, cfg common.CrawlerConfig) error {
	processor := &DefaultMessageProcessor{}
	fetcher := &DefaultMessageFetcher{}
	return processAllMessagesWithProcessor(tdlibClient, info, crawlID, channelUsername, sm, processor, fetcher, cfg)
}

// processAllMessagesWithProcessor retrieves and processes all messages from a channel.
// This function fetches messages in batches, starting from the most recent message
// and working backward through the history. It uses the provided MessageProcessor
// to process each message, allowing for customized message handling.
func processAllMessagesWithProcessor(
	tdlibClient crawler.TDLibClient,
	info *channelInfo,
	crawlID,
	channelUsername string,
	sm state.StateManager,
	processor MessageProcessor,
	fetcher MessageFetcher,
	cfg common.CrawlerConfig) error {

	var fromMessageID int64 = 0

	for {
		log.Info().Msgf("Fetching from message id %d", fromMessageID)

		messages, err := fetcher.FetchMessages(tdlibClient, info.chat.Id, fromMessageID)
		if err != nil {
			return err
		}

		if len(messages) == 0 {
			log.Info().Msgf("No more messages found in the channel %s", channelUsername)
			break
		}

		// Process messages
		for _, message := range messages {
			if err := processor.ProcessMessage(tdlibClient, message, info, crawlID, channelUsername, &sm, cfg); err != nil {
				log.Error().Err(err).Msgf("Error processing message %d", message.Id)
				continue // Skip to next message on error
			}
		}

		// Update message ID for next batch
		fromMessageID = messages[len(messages)-1].Id
	}

	return nil
}

// fetchMessages retrieves a batch of messages from a chat
func fetchMessages(tdlibClient crawler.TDLibClient, chatID int64, fromMessageID int64) ([]*client.Message, error) {
	chatHistory, err := tdlibClient.GetChatHistory(&client.GetChatHistoryRequest{
		ChatId:        chatID,
		Limit:         100,
		FromMessageId: fromMessageID,
	})

	if err != nil {
		log.Error().Stack().Err(err).Msg("Failed to get chat history")
		return nil, err
	}

	return chatHistory.Messages, nil
}

// processMessage processes a single message
func processMessage(tdlibClient crawler.TDLibClient, message *client.Message, info *channelInfo, crawlID, channelUsername string, sm state.StateManager, cfg common.CrawlerConfig) error {
	// Get detailed message info
	detailedMessage, err := tdlibClient.GetMessage(&client.GetMessageRequest{
		MessageId: message.Id,
		ChatId:    message.ChatId,
	})
	if err != nil {
		log.Error().Stack().Err(err).Msgf("Failed to get detailed message %d", message.Id)
		return err
	}

	// Get message link
	messageLink, err := tdlibClient.GetMessageLink(&client.GetMessageLinkRequest{
		ChatId:    message.ChatId,
		MessageId: message.Id,
	})
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to get link for message %d", message.Id)
		// Continue anyway, this is not critical
	}

	// Parse and store the message
	_, err = telegramhelper.ParseMessage(
		crawlID,
		detailedMessage,
		messageLink,
		info.chatDetails,
		info.supergroup,
		info.supergroupInfo,
		int(info.messageCount),
		int(info.totalViews),
		channelUsername,
		tdlibClient,
		sm,
		cfg,
	)

	if err != nil {
		log.Error().Stack().Err(err).Msgf("Failed to parse message %d", message.Id)
		return err
	}

	return nil
}
