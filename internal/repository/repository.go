package repository

import (
	"context"
	"noise-server/internal/model"
)

type Store interface {
	Close() error
	Admin(context.Context) (string, []byte, error)
	CreateAdmin(context.Context, string, []byte) error
	PutSession(context.Context, string, int64) error
	HasSession(context.Context, string, int64) (bool, error)
	DeleteSession(context.Context, string) error
	Nodes(context.Context) ([]model.Node, error)
	Node(context.Context, string) (model.Node, error)
	NodeByToken(context.Context, string) (model.Node, error)
	SaveNode(context.Context, model.Node) error
	DeleteNode(context.Context, string) error
	Labels(context.Context, string) ([]string, error)
	Tasks(context.Context) ([]model.LatencyTask, error)
	SaveTask(context.Context, model.LatencyTask) error
	DeleteTask(context.Context, string) error
	NodeTasks(context.Context, string) ([]model.LatencyTask, error)
	AssignTasks(context.Context, string, []string) error
	Channels(context.Context) ([]model.Channel, error)
	SaveChannel(context.Context, model.Channel) error
	DeleteChannel(context.Context, string) error
	Settings(context.Context) (model.Settings, error)
	SaveSettings(context.Context, model.Settings) error
	Runtimes(context.Context) (map[string]model.Runtime, error)
	SaveReport(context.Context, string, model.Report, int64, string) (model.Runtime, bool, error)
	SaveLatency(context.Context, string, []model.LatencyResult) error
	Latency(context.Context, string, string, int64, int64, int64) ([]model.LatencyResult, error)
	LatestLatency(context.Context, string, string) (*model.LatencyResult, error)
	History(context.Context, string, int64, int64, int64) ([]model.Sample, error)
	PeriodTraffic(context.Context, string, string) (int64, int64, error)
	Cleanup(context.Context, int64, int64, string) error
}
