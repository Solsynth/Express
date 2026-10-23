package developmodule

import (
	"net/http"

	"github.com/gin-gonic/gin"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// registerBotRoutes mounts the bot account surfaces.
//
//	GET    /api/private/bots                        bots of a project
//	GET    /api/private/bots/:botId                 one bot
//	POST   /api/private/bots                        create (sudo)
//	PATCH  /api/private/bots/:botId                 update
//	DELETE /api/private/bots/:botId                 delete (sudo)
//	GET    /api/private/bots/:botId/keys            API keys
//	GET    /api/private/bots/:botId/keys/:keyId     one API key
//	POST   /api/private/bots/:botId/keys            create key (sudo)
//	POST   /api/private/bots/:botId/keys/:keyId/rotate rotate key (sudo)
//	DELETE /api/private/bots/:botId/keys/:keyId     revoke key (sudo)
//	GET    /api/private/bots/:botId/chat            chat config
//	PUT    /api/private/bots/:botId/chat            replace chat config
//	POST   /api/private/bots/:botId/chat/manifest   patch chat manifest
//	GET    /api/bots/:botId                         public transparency info
//	GET    /api/bots/:botId/developer               its developer
//	GET    /api/bots/:botId/chat                    public chat config
//	GET    /api/bots/:botId/commands                public command manifest
func registerBotRoutes(api *gin.RouterGroup, deps Deps) {
	viewer := deps.Deps.Authenticate(developapi.Option{})
	create := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionBotAccountsCreate, Sudo: true})
	update := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionBotAccountsUpdate})
	remove := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionBotAccountsDelete, Sudo: true})
	keys := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionBotAccountsKeysManage, Sudo: true})
	chat := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionBotAccountsChatManage})

	bots := api.Group("/private/bots")
	bots.GET("", viewer, listBots(deps))
	bots.GET("/:botId", viewer, getBot(deps))
	bots.POST("", create, createBot(deps))
	bots.PATCH("/:botId", update, updateBot(deps))
	bots.DELETE("/:botId", remove, deleteBot(deps))

	bots.GET("/:botId/keys", viewer, listBotKeys(deps))
	bots.GET("/:botId/keys/:keyId", viewer, getBotKey(deps))
	bots.POST("/:botId/keys", keys, createBotKey(deps))
	bots.POST("/:botId/keys/:keyId/rotate", keys, rotateBotKey(deps))
	bots.DELETE("/:botId/keys/:keyId", keys, deleteBotKey(deps))

	bots.GET("/:botId/chat", viewer, getChatConfig(deps))
	bots.PUT("/:botId/chat", chat, putChatConfig(deps))
	bots.POST("/:botId/chat/manifest", chat, patchChatManifest(deps))

	public := api.Group("/bots")
	public.GET("/:botId", publicBot(deps))
	public.GET("/:botId/developer", publicBotDeveloper(deps))
	public.GET("/:botId/chat", publicChatConfig(deps))
	public.GET("/:botId/commands", publicBotCommands(deps))
}

// scopeBot resolves the developer, project and bot for a console request.
func scopeBot(c *gin.Context, deps Deps, role gen.DyPublisherMemberRole) (*develop.DeveloperScope, *develop.BotAccount, bool) {
	scope, ok := deps.Deps.Scope(c, role)
	if !ok {
		return nil, nil, false
	}
	bot, err := deps.Service.LoadBot(scope.Project.ID, c.Param("botId"))
	if err != nil {
		developapi.WriteError(c, err)
		return nil, nil, false
	}
	return scope, bot, true
}

// botView is the console representation: the registry row plus the identity
// account it points at.
type botView struct {
	*develop.BotAccount
	Account any `json:"account,omitempty"`
}

func listBots(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleViewer)
		if !ok {
			return
		}
		bots, accounts, err := deps.Service.ListBots(c.Request.Context(), scope.Project.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		views := make([]botView, 0, len(bots))
		for index := range bots {
			views = append(views, botView{BotAccount: &bots[index], Account: accounts[bots[index].ID]})
		}
		c.JSON(http.StatusOK, views)
	}
}

func getBot(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, bot, ok := scopeBot(c, deps, roleViewer)
		if !ok {
			return
		}
		view := botView{BotAccount: bot}
		if accounts, err := deps.Service.LoadBotsAccounts(c.Request.Context(), []develop.BotAccount{*bot}); err == nil {
			view.Account = accounts[bot.ID]
		}
		c.JSON(http.StatusOK, view)
	}
}

func createBot(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.BotInput
		if !bindJSON(c, &input) {
			return
		}
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		account, _ := developapi.Account(c)
		quota, err := deps.Service.DeveloperQuota(c.Request.Context(), account.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if quota.Used >= quota.Total {
			developapi.WriteError(c, validationError("bot quota exceeded"))
			return
		}
		bot, err := deps.Service.CreateBot(c.Request.Context(), scope.Project, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, bot)
	}
}

func updateBot(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.BotUpdateInput
		if !bindJSON(c, &input) {
			return
		}
		_, bot, ok := scopeBot(c, deps, roleEditor)
		if !ok {
			return
		}
		updated, err := deps.Service.UpdateBot(c.Request.Context(), bot, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, updated)
	}
}

func deleteBot(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, bot, ok := scopeBot(c, deps, roleEditor)
		if !ok {
			return
		}
		if err := deps.Service.DeleteBot(c.Request.Context(), bot); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}

// --- API keys ---

func listBotKeys(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, bot, ok := scopeBot(c, deps, roleViewer)
		if !ok {
			return
		}
		keys, err := deps.Service.ListBotKeys(c.Request.Context(), bot.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, keys)
	}
}

func getBotKey(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, _, ok := scopeBot(c, deps, roleViewer); !ok {
			return
		}
		key, err := deps.Service.GetBotKey(c.Request.Context(), c.Param("keyId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, key)
	}
}

type createBotKeyRequest struct {
	Label string `json:"label"`
}

func createBotKey(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input createBotKeyRequest
		if !bindJSON(c, &input) {
			return
		}
		_, bot, ok := scopeBot(c, deps, roleEditor)
		if !ok {
			return
		}
		key, err := deps.Service.CreateBotKey(c.Request.Context(), bot.ID, input.Label)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, key)
	}
}

func rotateBotKey(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, _, ok := scopeBot(c, deps, roleEditor); !ok {
			return
		}
		key, err := deps.Service.RotateBotKey(c.Request.Context(), c.Param("keyId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, key)
	}
}

func deleteBotKey(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, _, ok := scopeBot(c, deps, roleEditor); !ok {
			return
		}
		if err := deps.Service.DeleteBotKey(c.Request.Context(), c.Param("keyId")); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}

// --- Chat config ---

func getChatConfig(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, bot, ok := scopeBot(c, deps, roleViewer)
		if !ok {
			return
		}
		config, err := deps.Service.GetChatConfig(bot.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, config)
	}
}

func putChatConfig(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.BotChatConfig
		if !bindJSON(c, &input) {
			return
		}
		_, bot, ok := scopeBot(c, deps, roleEditor)
		if !ok {
			return
		}
		config, err := deps.Service.SaveChatConfig(bot.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		publishChatConfigUpdated(c, deps, bot.ID)
		c.JSON(http.StatusOK, config)
	}
}

func patchChatManifest(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.BotManifestInput
		if !bindJSON(c, &input) {
			return
		}
		_, bot, ok := scopeBot(c, deps, roleEditor)
		if !ok {
			return
		}
		config, err := deps.Service.PatchChatConfig(bot.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		publishChatConfigUpdated(c, deps, bot.ID)
		c.JSON(http.StatusOK, config)
	}
}

// --- Public bot surface ---

func publicBot(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		bot, err := deps.Service.LoadBotByID(c.Param("botId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		view := botView{BotAccount: bot}
		if accounts, err := deps.Service.LoadBotsAccounts(c.Request.Context(), []develop.BotAccount{*bot}); err == nil {
			view.Account = accounts[bot.ID]
		}
		response := gin.H{"bot": view}
		if bot.Project != nil {
			if developer, err := deps.Service.LoadDeveloperByID(c.Request.Context(), bot.Project.DeveloperID); err == nil {
				response["developer"] = developer
			}
		}
		c.JSON(http.StatusOK, response)
	}
}

func publicBotDeveloper(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		bot, err := deps.Service.LoadBotByID(c.Param("botId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if bot.Project == nil {
			developapi.WriteError(c, service.ErrNotFound)
			return
		}
		developer, err := deps.Service.LoadDeveloperByID(c.Request.Context(), bot.Project.DeveloperID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, developer)
	}
}

func publicChatConfig(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		bot, err := deps.Service.LoadBotByID(c.Param("botId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		config, err := deps.Service.PeekChatConfig(bot.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if config == nil {
			defaults := develop.DefaultChatConfig(bot.ID)
			c.JSON(http.StatusOK, defaults)
			return
		}
		c.JSON(http.StatusOK, config)
	}
}

func publicBotCommands(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		bot, err := deps.Service.LoadBotByID(c.Param("botId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		config, err := deps.Service.PeekChatConfig(bot.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		commands := []develop.BotCommand{}
		if config != nil {
			commands = config.Commands
		}
		c.JSON(http.StatusOK, commands)
	}
}
