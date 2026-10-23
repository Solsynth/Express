package developmodule

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// botChatConfigEvent is the wire shape Messager consumes: the C# EventBase
// envelope plus the bot account id and update instant.
type botChatConfigEvent struct {
	EventID      string    `json:"event_id"`
	Timestamp    time.Time `json:"timestamp"`
	EventType    string    `json:"event_type"`
	StreamName   string    `json:"stream_name"`
	BotAccountID string    `json:"bot_account_id"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// publishChatConfigUpdated notifies the fleet that a bot's chat config changed.
// Messager invalidates its command cache from this event; a nil bus is a no-op.
func publishChatConfigUpdated(c *gin.Context, deps Deps, botID string) {
	bus := deps.Events
	if bus == nil {
		return
	}
	now := time.Now().UTC()
	event := botChatConfigEvent{
		EventID:      newEventID(),
		Timestamp:    now,
		EventType:    "bot.chat.config.updated",
		StreamName:   "bot_chat_events",
		BotAccountID: botID,
		UpdatedAt:    now,
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	_ = bus.PublishJetStream(ctx, "bot.chat.config.updated", "bot_chat_events", event)
}
