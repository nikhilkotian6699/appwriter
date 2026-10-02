package guild

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
)

// Guild runs workflows on top of the run engine.
type Guild struct {
	engine  *runs.Engine
	tracker *runs.Tracker
	q       *sqlcgen.Queries
	appName string
	// CallTimeout bounds one gateway call of one writer.
	CallTimeout time.Duration
}

// New wires a Guild.
func New(engine *runs.Engine, q *sqlcgen.Queries, appName string, callTimeout time.Duration) *Guild {
	if callTimeout <= 0 {
		callTimeout = 2 * time.Minute
	}
	return &Guild{engine: engine, tracker: engine.Tracker(), q: q, appName: appName, CallTimeout: callTimeout}
}

// Tracker exposes the run tracker.
func (g *Guild) Tracker() *runs.Tracker { return g.tracker }

// Engine exposes the run engine.
func (g *Guild) Engine() *runs.Engine { return g.engine }

// FriendlyError turns a gateway failure into a sentence for the UI without
// leaking anything secret.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	var ge *llm.GatewayError
	if errors.As(err, &ge) {
		switch {
		case ge.Status == 401 || ge.Status == 403:
			return fmt.Sprintf("the gateway refused the request (%d): check LITELLM_API_KEY", ge.Status)
		case ge.Status == 404 || (ge.Status == 400 && strings.Contains(strings.ToLower(ge.Message), "model")):
			return fmt.Sprintf("the gateway does not know this alias (%d): %s", ge.Status, ge.Message)
		case ge.Status == 429:
			return "the gateway is rate-limiting requests (429); it was retried and still refused"
		case ge.Status >= 500:
			return fmt.Sprintf("the gateway failed upstream (%d): %s", ge.Status, ge.Message)
		default:
			return fmt.Sprintf("the gateway returned %d: %s", ge.Status, ge.Message)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the gateway did not answer in time"
	}
	if errors.Is(err, context.Canceled) {
		return "the request was cancelled"
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return "the gateway did not answer in time"
		}
		return "could not reach the gateway: " + err.Error()
	}
	return err.Error()
}
