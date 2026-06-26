package hitl

import (
	"sync"
	"time"

	"clipdev/internal/config"
	"clipdev/internal/parser"
)

type Decision struct {
	Approved bool
	Note     string
}

type PendingOp struct {
	ID       string
	Cmd      *parser.Command
	Decision chan Decision
	timer    *time.Timer
}

type Manager struct {
	cfg     *config.Config
	pending sync.Map
}

func New(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) Requires(op string) bool {
	for _, o := range m.cfg.HITL.RequireApproval {
		if o == op {
			return true
		}
	}
	return false
}

func (m *Manager) Submit(cmd *parser.Command) *PendingOp {
	ch := make(chan Decision, 1)
	p := &PendingOp{ID: cmd.ID, Cmd: cmd, Decision: ch}

	m.pending.Store(cmd.ID, p)

	if m.cfg.HITL.AutoReject {
		p.timer = time.AfterFunc(m.cfg.HITL.Timeout.Duration, func() {
			m.Reject(cmd.ID)
		})
	}

	return p
}

func (m *Manager) Approve(id string) bool {
	v, ok := m.pending.Load(id)
	if !ok {
		return false
	}
	p := v.(*PendingOp)
	if p.timer != nil {
		p.timer.Stop()
	}
	m.pending.Delete(id)
	select {
	case p.Decision <- Decision{Approved: true}:
	default:
	}
	return true
}

func (m *Manager) Reject(id string) bool {
	v, ok := m.pending.Load(id)
	if !ok {
		return false
	}
	p := v.(*PendingOp)
	if p.timer != nil {
		p.timer.Stop()
	}
	m.pending.Delete(id)
	select {
	case p.Decision <- Decision{Approved: false}:
	default:
	}
	return true
}

func (m *Manager) TimeoutSecs(id string) int {
	v, ok := m.pending.Load(id)
	if !ok {
		return 0
	}
	_ = v.(*PendingOp)
	return int(m.cfg.HITL.Timeout.Duration.Seconds())
}

func (m *Manager) Wait(p *PendingOp) Decision {
	return <-p.Decision
}
