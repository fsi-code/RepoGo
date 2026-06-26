package watcher

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync/atomic"

	"clipdev/internal/broker"
	"clipdev/internal/clip"
	"clipdev/internal/dispatch"
	"clipdev/internal/ops"
	"clipdev/internal/parser"
)

type Watcher struct {
	disp        *dispatch.Dispatcher
	broker      *broker.Broker
	processing  atomic.Bool
	lastWritten atomic.Value // string — évite de retraiter nos propres réponses
}

func New(d *dispatch.Dispatcher, b *broker.Broker) *Watcher {
	return &Watcher{disp: d, broker: b}
}

func (w *Watcher) Start(ctx context.Context) {
	// Sur Windows : clip.Watch() utilise AddClipboardFormatListener (WM_CLIPBOARDUPDATE)
	// → notification OS pure, latence ~0ms, zéro polling.
	// Sur Linux  : goroutine interne à 200ms (xclip/wl-paste, sans CGo).
	clipCh := clip.Watch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case content, ok := <-clipCh:
			if !ok {
				return
			}
			// Ignorer les réponses que nous avons nous-mêmes écrites.
			if lw, _ := w.lastWritten.Load().(string); lw == content {
				continue
			}
			if w.processing.Load() {
				continue
			}

			cmds, _ := parser.Extract(content)
			if len(cmds) == 0 {
				continue
			}

			w.processing.Store(true)
			go func(cmds []*parser.Command) {
				defer w.processing.Store(false)
				w.process(cmds)
			}(cmds)
		}
	}
}

func (w *Watcher) process(cmds []*parser.Command) {
	if len(cmds) == 1 {
		cmd := cmds[0]
		w.broker.BroadcastReceived(cmd.Op, 1)
		resp := w.disp.Dispatch(cmd)
		w.broadcastResult(resp)
		w.writeResponse([]*ops.Response{resp})
		return
	}

	w.broker.BroadcastReceived(cmds[0].Op, len(cmds))
	statuses := make([]string, len(cmds))
	for i := range statuses {
		statuses[i] = "waiting"
	}
	w.broker.BroadcastPipeline(cmds, statuses)

	var results []*ops.Response
	for i, cmd := range cmds {
		statuses[i] = "running"
		w.broker.BroadcastPipeline(cmds, statuses)

		resp := w.disp.Dispatch(cmd)
		results = append(results, resp)

		if resp.OK {
			statuses[i] = "done"
		} else {
			statuses[i] = "error"
		}
		w.broadcastResult(resp)
		w.broker.BroadcastPipeline(cmds, statuses)

		if !resp.OK {
			log.Printf("pipeline stopped at step %d: %s", i+1, resp.Error)
			break
		}
	}

	w.writeResponse(results)
}

func (w *Watcher) broadcastResult(resp *ops.Response) {
	if resp.OK {
		var resultStr string
		json.Unmarshal(resp.Result, &resultStr)
		w.broker.BroadcastDone(resp.Op, truncate(resultStr, 80), resp.DurationMs)
	} else {
		w.broker.BroadcastError(resp.Op, resp.Error)
	}
}

func (w *Watcher) writeResponse(responses []*ops.Response) {
	var data []byte
	var err error

	if len(responses) == 1 {
		data, err = json.MarshalIndent(responses[0], "", "  ")
	} else {
		type pipelineResult struct {
			Clipdev string          `json:"_clipdev"`
			Type    string          `json:"type"`
			Results []*ops.Response `json:"results"`
		}
		data, err = json.MarshalIndent(pipelineResult{
			Clipdev: "1.0",
			Type:    "pipeline",
			Results: responses,
		}, "", "  ")
	}
	if err != nil {
		log.Printf("marshal response: %v", err)
		return
	}

	payload := string(data)
	// Enregistrer avant d'écrire pour que le hook clipboard ignore notre propre réponse.
	w.lastWritten.Store(payload)
	if werr := clip.Write(payload); werr != nil {
		log.Printf("write clipboard: %v", werr)
	}
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
