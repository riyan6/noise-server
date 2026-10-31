package httpapi

import (
	"noise-server/internal/model"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *API) agentInfo(c *gin.Context) {
	var input model.AgentInfo
	if !bind(c, &input) {
		return
	}
	done(c, a.service.UpdateInfo(c.Request.Context(), c.GetString("nodeID"), input))
}
func (a *API) agentConfig(c *gin.Context) {
	v, e := a.service.AgentConfig(c.Request.Context(), c.GetString("nodeID"))
	reply(c, v, e)
}
func (a *API) report(c *gin.Context) {
	var r model.Report
	if !bind(c, &r) {
		return
	}
	runtime, e := a.service.Report(c.Request.Context(), c.GetString("nodeID"), r)
	reply(c, gin.H{"receivedAt": runtime.ReceivedAt, "configVersion": strconv.FormatUint(a.service.Version(), 10)}, e)
}
func (a *API) latencyResults(c *gin.Context) {
	var input struct {
		Results []model.LatencyResult `json:"results"`
	}
	if !bind(c, &input) {
		return
	}
	done(c, a.service.LatencyResults(c.Request.Context(), c.GetString("nodeID"), input.Results))
}
