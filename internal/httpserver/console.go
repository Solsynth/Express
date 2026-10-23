package httpserver

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// consolePublisherView is one publisher the signed-in account can act for.
type consolePublisherView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Nick    string `json:"nick,omitempty"`
	Role    string `json:"role"`
	Picture string `json:"picture,omitempty"`
}

type consoleAccountView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Nick        string `json:"nick,omitempty"`
	IsSuperuser bool   `json:"is_superuser"`
	Picture     string `json:"picture,omitempty"`
}

type consoleSessionView struct {
	Account     consoleAccountView     `json:"account"`
	Publishers  []consolePublisherView `json:"publishers"`
	Permissions []string               `json:"permissions"`
}

// consolePermissions are the nodes the console asks about in one session
// lookup. Every entry gates a control; a node the account does not hold
// simply hides that control instead of letting it fail with 403.
var consolePermissions = []string{
	service.PermissionProductsCreate,
	service.PermissionProductsUpdate,
	service.PermissionProductsDelete,
	service.PermissionArtifactsUpload,
	service.PermissionReleasesManage,
	service.PermissionReleasesPublish,
	service.PermissionChannelsManage,
	service.PermissionUploadKeysManage,
	service.PermissionMetricsRead,
	service.PermissionSyncManage,
}

// registerConsoleRoutes mounts the console's own session surface. It answers
// who the cookie belongs to and what that account may operate, so the SPA can
// render its navigation without probing every gated route.
func registerConsoleRoutes(engine *gin.Engine, releases *service.ReleaseService, publishers service.PublisherDirectory) {
	if engine == nil {
		return
	}
	engine.GET("/api/console/session", func(c *gin.Context) {
		token, ok := requestAuthToken(c)
		if !ok {
			writeError(c, service.ErrUnauthorized)
			return
		}
		account, err := authenticatePublisher(c.Request.Context(), publishers, token)
		accountID := strings.TrimSpace(account.ID)
		if err != nil || accountID == "" {
			writeError(c, service.ErrUnauthorized)
			return
		}

		view := consoleSessionView{
			Account: consoleAccountView{
				ID:          accountID,
				Name:        account.Name,
				Nick:        account.Nick,
				IsSuperuser: account.IsSuperuser,
				Picture:     account.PictureURL,
			},
			Publishers:  []consolePublisherView{},
			Permissions: []string{},
		}

		// A directory that cannot enumerate memberships still serves the
		// marketplace; the console then offers no publisher workspace rather
		// than failing the whole session.
		if directory, ok := publishers.(service.MembershipDirectory); ok {
			memberships, err := directory.PublisherMemberships(c.Request.Context(), accountID)
			if err != nil {
				writeError(c, service.ErrDependency)
				return
			}
			for _, membership := range memberships {
				publisher := membership.Publisher
				if publisher == nil {
					continue
				}
				view.Publishers = append(view.Publishers, consolePublisherView{
					ID:      publisher.GetId(),
					Name:    publisher.GetName(),
					Nick:    publisher.GetNick(),
					Role:    publisherRoleName(membership.Role),
					Picture: publisher.GetPicture().GetUrl(),
				})
			}
		}

		// Superusers bypass the fine-grained check on the server, so the
		// console reports the same effective set instead of an empty list.
		if account.IsSuperuser {
			view.Permissions = append(view.Permissions, consolePermissions...)
		} else {
			for _, key := range consolePermissions {
				if err := releases.RequireAccountPermission(c.Request.Context(), accountID, key); err == nil {
					view.Permissions = append(view.Permissions, key)
				}
			}
		}

		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, view)
	})
}

// publisherRoleName maps the membership role to the lowercase word the console
// prints. An unknown role is reported verbatim so a newer Sphere stays legible.
func publisherRoleName(role gen.DyPublisherMemberRole) string {
	switch role {
	case gen.DyPublisherMemberRole_DY_OWNER:
		return "owner"
	case gen.DyPublisherMemberRole_DY_MANAGER:
		return "manager"
	case gen.DyPublisherMemberRole_DY_EDITOR:
		return "editor"
	case gen.DyPublisherMemberRole_DY_VIEWER:
		return "viewer"
	default:
		return "member"
	}
}
