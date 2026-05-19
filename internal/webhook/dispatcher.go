package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/version"
)

// ConfigSource provides the current set of webhook configurations.
// Implementations may return a different slice on each call.
type ConfigSource interface {
	Webhooks() []core.WebhookConfig
}

// Dispatcher consumes SessionEvents from a bus and delivers them as HTTP
// POSTs to configured webhooks.
type Dispatcher struct {
	bus     *core.EventBus
	cfg     ConfigSource
	client  *http.Client
	apiAddr func() string

	queueSize int
	workers   int
	backoffs  []time.Duration
}

type deliveryJob struct {
	webhook    core.WebhookConfig
	body       []byte
	deliveryID string
}

// New creates a Dispatcher.
func New(bus *core.EventBus, cfg ConfigSource, client *http.Client, apiAddr func() string) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if apiAddr == nil {
		apiAddr = func() string { return "" }
	}
	return &Dispatcher{
		bus:       bus,
		cfg:       cfg,
		client:    client,
		apiAddr:   apiAddr,
		queueSize: 256,
		workers:   4,
		backoffs:  []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second},
	}
}

// Start subscribes to the bus and runs until ctx is cancelled.
func (d *Dispatcher) Start(ctx context.Context) error {
	events := d.bus.Subscribe(256)
	defer d.bus.Unsubscribe(events)

	queue := make(chan deliveryJob, d.queueSize)

	workerDone := make(chan struct{}, d.workers)
	for i := 0; i < d.workers; i++ {
		go func() {
			defer func() { workerDone <- struct{}{} }()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-queue:
					if !ok {
						return
					}
					d.deliver(ctx, job)
				}
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			close(queue)
			for i := 0; i < d.workers; i++ {
				<-workerDone
			}
			return ctx.Err()
		case ev := <-events:
			d.fanout(ev, queue)
		}
	}
}

func (d *Dispatcher) fanout(ev core.SessionEvent, queue chan<- deliveryJob) {
	webhooks := d.cfg.Webhooks()
	if len(webhooks) == 0 {
		return
	}
	deliveryID := newDeliveryID()
	payload := Payload{
		ID:          deliveryID,
		Event:       "state_transition",
		SessionID:   ev.SessionID,
		Agent:       ev.Agent,
		From:        string(ev.From),
		To:          string(ev.To),
		ProjectPath: ev.ProjectPath,
		Timestamp:   ev.At.UTC(),
	}
	if base := d.apiAddr(); base != "" {
		payload.API = &APILinks{
			SessionURL: fmt.Sprintf("%s/api/sessions/%s", base, ev.SessionID),
			DetailURL:  fmt.Sprintf("%s/api/sessions/%s/full", base, ev.SessionID),
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("webhook: marshal payload: %v", err)
		return
	}
	for _, w := range webhooks {
		if !w.IsEnabled() || !Matches(w, ev) {
			continue
		}
		select {
		case queue <- deliveryJob{webhook: w, body: body, deliveryID: deliveryID}:
		default:
			log.Printf("webhook: queue full, dropping delivery for %q", w.Name)
		}
	}
}

// deliver performs the POST, retrying on transient failures.
// 4xx is treated as permanent. 5xx, network errors, and timeouts retry
// with backoff up to len(d.backoffs) times (total attempts = 1 + retries).
func (d *Dispatcher) deliver(ctx context.Context, job deliveryJob) {
	for attempt := 0; attempt <= len(d.backoffs); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d.backoffs[attempt-1]):
			}
		}
		status, transient, err := d.doOnce(ctx, job)
		if err == nil && !transient {
			return
		}
		if err == nil && status >= 400 && status < 500 {
			return
		}
		if attempt == len(d.backoffs) {
			log.Printf("webhook %q: giving up after %d attempts", job.webhook.Name, attempt+1)
		}
	}
}

// doOnce performs a single POST. Returns (status, transient, err).
//   - 2xx: status=2xx, transient=false, err=nil → success
//   - 4xx: status=4xx, transient=false, err=nil → permanent
//   - 5xx: status=5xx, transient=true, err=nil → retry
//   - network/timeout: status=0, transient=true, err=non-nil → retry
func (d *Dispatcher) doOnce(ctx context.Context, job deliveryJob) (int, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.webhook.URL, bytes.NewReader(job.body))
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "lazyagent/"+version.String())
	req.Header.Set("X-Lazyagent-Event", "state_transition")
	req.Header.Set("X-Lazyagent-Delivery", job.deliveryID)
	if job.webhook.Secret != "" {
		req.Header.Set("X-Lazyagent-Signature", Sign(job.webhook.Secret, job.body))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, false, nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	log.Printf("webhook %q: %d %s — %s", job.webhook.Name, resp.StatusCode, resp.Status, string(snippet))
	if resp.StatusCode >= 500 {
		return resp.StatusCode, true, nil
	}
	return resp.StatusCode, false, nil
}

func newDeliveryID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
