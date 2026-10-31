package httpapi

import (
	"noise-server/internal/model"
	"noise-server/internal/service"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *API) nodes(c *gin.Context) {
	nodes, e := a.service.Nodes(c.Request.Context())
	if e != nil {
		respondError(c, e)
		return
	}
	filtered := []model.AdminNode{}
	query := strings.ToLower(strings.TrimSpace(c.Query("q")))
	for _, n := range nodes {
		if (c.Query("brand") == "" || n.Brand == c.Query("brand")) && (c.Query("group") == "" || n.Group == c.Query("group")) {
			ip := ""
			if n.Agent != nil {
				ip = n.Agent.IP
			}
			if strings.Contains(strings.ToLower(n.Name+" "+n.Brand+" "+n.Group+" "+n.Note+" "+ip), query) {
				filtered = append(filtered, n)
			}
		}
	}
	success(c, filtered)
}
func (a *API) createNode(c *gin.Context) {
	var input struct {
		Name  string `json:"name"`
		Brand string `json:"brand"`
	}
	if !bind(c, &input) {
		return
	}
	v, e := a.service.CreateNode(c.Request.Context(), input.Name, input.Brand)
	reply(c, v, e)
}
func (a *API) node(c *gin.Context) {
	v, e := a.service.Node(c.Request.Context(), c.Param("id"))
	reply(c, v, e)
}
func (a *API) updateNode(c *gin.Context) {
	var p service.NodeChanges
	if !bind(c, &p) {
		return
	}
	v, e := a.service.UpdateNode(c.Request.Context(), c.Param("id"), p)
	reply(c, v, e)
}
func (a *API) deleteNode(c *gin.Context) {
	done(c, a.service.DeleteNode(c.Request.Context(), c.Param("id")))
}
func (a *API) labels(kind string) gin.HandlerFunc {
	return func(c *gin.Context) { v, e := a.service.Labels(c.Request.Context(), kind); reply(c, v, e) }
}
func (a *API) billing(c *gin.Context) {
	var b model.Billing
	if !bind(c, &b) {
		return
	}
	done(c, a.service.Billing(c.Request.Context(), c.Param("id"), b))
}
func (a *API) traffic(c *gin.Context) {
	var t model.TrafficLimit
	if !bind(c, &t) {
		return
	}
	done(c, a.service.Traffic(c.Request.Context(), c.Param("id"), t))
}
func (a *API) install(c *gin.Context) {
	v, e := a.service.Install(c.Request.Context(), c.Param("id"))
	reply(c, v, e)
}
func (a *API) tasks(c *gin.Context) { v, e := a.service.Tasks(c.Request.Context()); reply(c, v, e) }
func (a *API) saveTask(c *gin.Context) {
	var p service.TaskChanges
	if !bind(c, &p) {
		return
	}
	v, e := a.service.SaveTask(c.Request.Context(), c.Param("id"), p)
	reply(c, v, e)
}
func (a *API) deleteTask(c *gin.Context) {
	done(c, a.service.DeleteTask(c.Request.Context(), c.Param("id")))
}
func (a *API) assign(c *gin.Context) {
	var input struct {
		TaskIDs []string `json:"taskIds"`
	}
	if !bind(c, &input) {
		return
	}
	if input.TaskIDs == nil {
		fail(c, 400, "invalid_request", "taskIds must be an array; use [] to clear assignments")
		return
	}
	done(c, a.service.Assign(c.Request.Context(), c.Param("id"), input.TaskIDs))
}
func (a *API) channels(c *gin.Context) {
	v, e := a.service.Channels(c.Request.Context())
	reply(c, v, e)
}
func (a *API) saveChannel(c *gin.Context) {
	var p service.ChannelChanges
	if !bind(c, &p) {
		return
	}
	v, e := a.service.SaveChannel(c.Request.Context(), c.Param("id"), p)
	reply(c, v, e)
}
func (a *API) deleteChannel(c *gin.Context) {
	done(c, a.service.DeleteChannel(c.Request.Context(), c.Param("id")))
}
func (a *API) testChannel(c *gin.Context) {
	var input struct {
		ChannelID string                `json:"channelId"`
		Config    *model.TelegramConfig `json:"config"`
	}
	if !bind(c, &input) {
		return
	}
	v, e := a.service.TestChannel(c.Request.Context(), input.ChannelID, input.Config)
	reply(c, v, e)
}
func (a *API) testSavedChannel(c *gin.Context) {
	v, e := a.service.TestChannel(c.Request.Context(), c.Param("id"), nil)
	reply(c, v, e)
}
func (a *API) settings(c *gin.Context) { success(c, a.service.Settings()) }
func (a *API) saveSettings(c *gin.Context) {
	var v model.Settings
	if !bind(c, &v) {
		return
	}
	done(c, a.service.SaveSettings(c.Request.Context(), v))
}
func (a *API) geoip(c *gin.Context) {
	var input struct {
		Provider  string `json:"provider"`
		IP        string `json:"ip"`
		UserAgent string `json:"userAgent"`
	}
	if !bind(c, &input) {
		return
	}
	v, e := a.service.GeoLookup(c.Request.Context(), input.Provider, input.IP, input.UserAgent)
	reply(c, v, e)
}
