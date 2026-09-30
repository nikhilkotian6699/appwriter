package guild

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"writersguild/internal/llm"
	"writersguild/internal/runs"
)

// Guild runs workflows on top of the run tracker.
type Guild struct {
	tracker *runs.Tracker
	appName string
}

// New wires a Guild.
func New(tracker *runs.Tracker, appName string) *Guild {
	return &Guild{tracker: tracker, appName: appName}
}

// Tracker exposes the run tracker.
func (g *Guild) Tracker() *runs.Tracker { return g.tracker }

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
