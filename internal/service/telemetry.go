package service

import (
	"context"
	"math"
	"noise-server/internal/model"
	"time"
)

func (s *Service) Report(ctx context.Context, id string, r model.Report) (model.Runtime, error) {
	if e := validateReport(r); e != nil {
		return model.Runtime{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node, e := s.store.Node(ctx, id)
	if e != nil {
		return model.Runtime{}, e
	}
	period := PeriodStart(node.Traffic.ResetDay, s.Now().In(s.Location))
	runtime, fresh, e := s.store.SaveReport(ctx, id, r, s.Now().UnixMilli(), period)
	if e != nil {
		return runtime, e
	}
	if fresh {
		s.cache.Set(id, runtime)
		if s.renew(&node) {
			if e = s.store.SaveNode(ctx, node); e != nil {
				return runtime, e
			}
		}
	}
	return runtime, nil
}
func (s *Service) LatencyResults(ctx context.Context, id string, results []model.LatencyResult) error {
	if len(results) == 0 || len(results) > 1000 {
		return invalid("results must contain 1–1000 probes")
	}
	now := s.Now().UnixMilli()
	cutoff := s.Now().Add(-Retention).UnixMilli()
	for i := range results {
		r := &results[i]
		if !text(r.RunID, 128, true) || !text(r.TaskID, 128, true) || !contains(r.Status, "ok", "timeout", "error") {
			return invalid("invalid latency result identity or status")
		}
		if r.Time == 0 {
			r.Time = now
		}
		if r.Time < cutoff || r.Time > now+5000 {
			return invalid("probe time is outside the allowed seven-day window")
		}
		if r.Time > now {
			r.Time = now
		}
		if r.Status == "ok" {
			if r.Value == nil || math.IsNaN(*r.Value) || math.IsInf(*r.Value, 0) || *r.Value < 0 || *r.Value > 5000 {
				return invalid("successful latency must be between 0 and 5000 milliseconds")
			}
		} else if r.Value != nil {
			return invalid("failed probes must have a null value")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.SaveLatency(ctx, id, results)
}
func (s *Service) PublicNodes(ctx context.Context) ([]model.PublicNode, error) {
	nodes, e := s.store.Nodes(ctx)
	if e != nil {
		return nil, e
	}
	result := make([]model.PublicNode, 0, len(nodes))
	for _, n := range nodes {
		value, e := s.publicNode(ctx, n)
		if e != nil {
			return nil, e
		}
		result = append(result, value)
	}
	return result, nil
}
func (s *Service) PublicNode(ctx context.Context, id string) (model.PublicNode, error) {
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return model.PublicNode{}, e
	}
	return s.publicNode(ctx, n)
}
func (s *Service) publicNode(ctx context.Context, n model.Node) (model.PublicNode, error) {
	result := model.PublicNode{ID: n.ID, Name: n.Name, Provider: n.Brand, Group: n.Group, Billing: model.PublicBilling{Cycle: n.Billing.Cycle, Price: n.Billing.Price, Currency: n.Billing.Currency, ExpiresAt: n.Billing.ExpiresAt}, Traffic: n.Traffic, Tasks: []model.PublicTask{}}
	if a := n.Agent; a != nil {
		result.Region = a.Region
		result.City = a.City
		result.OS = a.OS
		result.Kernel = a.Kernel
		result.Arch = a.Arch
		result.CPUModel = a.CPUModel
		result.Cores = a.Cores
		result.Memory = a.Memory
		result.Storage = a.Storage
	}
	tasks, e := s.store.NodeTasks(ctx, n.ID)
	if e != nil {
		return result, e
	}
	for _, t := range tasks {
		result.Tasks = append(result.Tasks, model.PublicTask{ID: t.ID, Name: t.Name, Type: t.Type, Interval: t.Interval})
	}
	if r, ok := s.cache.Get(n.ID); ok {
		result.LastSeen = &r.ReceivedAt
		result.Online = s.online(r)
		result.Total = r.TotalUp + r.TotalDown
		if result.Online {
			result.Uptime = r.Report.Uptime
			result.CPU = &r.Report.CPU
			result.Mem = &r.Report.Mem
			result.Disk = &r.Report.Disk
			result.Up = &r.Report.Up
			result.Down = &r.Report.Down
			result.Load = &r.Report.Load
			if len(tasks) > 0 {
				probe, e := s.store.LatestLatency(ctx, n.ID, tasks[0].ID)
				if e != nil {
					return result, e
				}
				if probe != nil && probe.Status == "ok" && s.Now().UnixMilli()-probe.Time <= int64(tasks[0].Interval+ProbeTimeout)*1000 {
					result.RTT = probe.Value
				}
			}
		}
	}
	result.PeriodStart = PeriodStart(n.Traffic.ResetDay, s.Now().In(s.Location))
	result.PeriodUp, result.PeriodDown, e = s.store.PeriodTraffic(ctx, n.ID, result.PeriodStart)
	if e != nil {
		return result, e
	}
	result.PeriodUsed = trafficUsed(n.Traffic.Method, result.PeriodUp, result.PeriodDown)
	return result, nil
}

type Window struct{ From, To, Step int64 }

func (s *Service) Window(from, to, step int64) (Window, error) {
	now := s.Now().UnixMilli()
	cutoff := s.Now().Add(-Retention).UnixMilli()
	if to == 0 {
		to = now
	}
	if from == 0 {
		from = to - int64(time.Hour/time.Millisecond)
	}
	if to > now {
		to = now
	}
	if from < cutoff {
		from = cutoff
	}
	if from >= to || step < 0 || step > int64(Retention/time.Millisecond) {
		return Window{}, invalid("history requires from < to and a step within seven days")
	}
	step = max(1000, step, ((to-from+999999)/1000000)*1000)
	return Window{From: from, To: to, Step: step}, nil
}
func (s *Service) History(ctx context.Context, id string, w Window) ([]model.Sample, error) {
	if _, e := s.store.Node(ctx, id); e != nil {
		return nil, e
	}
	samples, e := s.store.History(ctx, id, w.From, w.To, w.Step)
	if e != nil {
		return nil, e
	}
	tasks, e := s.store.NodeTasks(ctx, id)
	if e != nil {
		return nil, e
	}
	if len(tasks) > 0 {
		points, e := s.store.Latency(ctx, id, tasks[0].ID, w.From, w.To, w.Step)
		if e != nil {
			return nil, e
		}
		index := -1
		for i := range samples {
			for index+1 < len(points) && points[index+1].Time <= samples[i].Time {
				index++
			}
			if index >= 0 {
				point := points[index]
				if point.Status == "ok" && samples[i].Time-point.Time <= int64(tasks[0].Interval+ProbeTimeout)*1000 {
					samples[i].RTT = point.Value
				}
			}
		}
	}
	result := make([]model.Sample, 0, len(samples))
	threshold := max(w.Step*2, int64(max(10, s.Settings().Refresh*3))*1000)
	for i, p := range samples {
		if i > 0 && p.Time-samples[i-1].Time > threshold {
			result = append(result, model.Sample{Time: samples[i-1].Time + 1})
		}
		result = append(result, p)
	}
	return result, nil
}

type PublicLatencyPoint struct {
	Time   int64    `json:"time"`
	Status string   `json:"status"`
	Value  *float64 `json:"value"`
}
type PublicLatencySeries struct {
	Task    model.PublicTask     `json:"task"`
	Records []PublicLatencyPoint `json:"records"`
}

func (s *Service) PublicLatency(ctx context.Context, node, taskID string, w Window) ([]PublicLatencySeries, error) {
	if _, e := s.store.Node(ctx, node); e != nil {
		return nil, e
	}
	tasks, e := s.store.NodeTasks(ctx, node)
	if e != nil {
		return nil, e
	}
	result := []PublicLatencySeries{}
	for _, t := range tasks {
		if taskID != "" && taskID != t.ID {
			continue
		}
		points, e := s.store.Latency(ctx, node, t.ID, w.From, w.To, w.Step)
		if e != nil {
			return nil, e
		}
		series := PublicLatencySeries{Task: model.PublicTask{ID: t.ID, Name: t.Name, Type: t.Type, Interval: t.Interval}, Records: []PublicLatencyPoint{}}
		for _, p := range points {
			if p.Time >= w.From {
				series.Records = append(series.Records, PublicLatencyPoint{Time: p.Time, Status: p.Status, Value: p.Value})
			}
		}
		result = append(result, series)
	}
	if taskID != "" && len(result) == 0 {
		return nil, model.ErrNotFound
	}
	return result, nil
}
func (s *Service) Maintain(ctx context.Context) error {
	now := s.Now()
	s.mu.Lock()
	nodes, e := s.store.Nodes(ctx)
	if e == nil {
		for _, node := range nodes {
			if r, ok := s.cache.Get(node.ID); ok && s.online(r) && s.renew(&node) {
				if e = s.store.SaveNode(ctx, node); e != nil {
					break
				}
			}
		}
	}
	s.mu.Unlock()
	if e != nil {
		return e
	}
	// Traffic periods are billing aggregates, not probe history; keep the current/previous month.
	old := now.In(s.Location).AddDate(0, -2, 0).Format("2006-01-02")
	return s.store.Cleanup(ctx, now.Add(-Retention).UnixMilli(), now.UnixMilli(), old)
}
