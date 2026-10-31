package service

import (
	"context"
	"fmt"
	"strings"
)

type Installation struct {
	NodeID  string `json:"nodeId"`
	Token   string `json:"token"`
	Command string `json:"command"`
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func (s *Service) Install(ctx context.Context, id string) (Installation, error) {
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return Installation{}, e
	}
	agent := s.Settings().Agent
	if agent.ScriptURL == "" {
		return Installation{}, invalid("configure the agent install script URL in settings first")
	}
	return Installation{NodeID: n.ID, Token: n.Token, Command: fmt.Sprintf("wget -qO- -- %s | sudo bash -s -- -e %s -t %s --month-rotate %d", shellQuote(agent.ScriptURL), shellQuote(agent.Endpoint), shellQuote(n.Token), n.Traffic.ResetDay)}, nil
}
