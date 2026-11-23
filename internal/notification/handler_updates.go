package notification

import (
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/coder/websocket"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// maxSubscribedNotifications bounds the notifications one connection follows by ID; a batch is followed by its ID.
const maxSubscribedNotifications = 100

// pingInterval is how often a connection is pinged, so a client that vanished without closing is noticed.
const pingInterval = 30 * time.Second

// updateWriteTimeout bounds one write to a client, the event or the ping.
const updateWriteTimeout = 5 * time.Second

// statusUpdates answers GET /v1/status-updates by taking the connection over as a WebSocket and sending, as JSON
// text messages, the status changes of the notifications and the batch the query names (ADR 0033). Everything is
// checked before the connection is taken over, so a refusal is the usual problem. The client sends nothing; a
// message from it closes the connection.
func (h *Handler) statusUpdates(w http.ResponseWriter, r *http.Request) error {
	filter, err := parseStatusFilter(r.URL.Query())
	if err != nil {
		return err
	}
	if !hasToken(r.Header, "Connection", "upgrade") || !hasToken(r.Header, "Upgrade", "websocket") {
		return httpx.NewProblem(http.StatusUpgradeRequired, "upgrade_required", "this endpoint is a WebSocket; upgrade the connection")
	}
	if !sameOrigin(r) {
		return httpx.NewProblem(http.StatusForbidden, "origin_not_allowed", "a page may follow status updates only from the API's own origin")
	}
	// Subscribed before the handshake completes, so a change the client causes right after connecting is not missed.
	sub := h.hub.Subscribe(filter)
	defer h.hub.Unsubscribe(sub)
	// Taking the connection over clears the server's read and write deadlines (net/http does it on hijack); each
	// write below has a timeout of its own, and pings notice a vanished client.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has answered the client.
		h.log.WarnContext(r.Context(), "status updates not accepted", "error", err)
		return nil
	}
	defer func() { _ = conn.CloseNow() }()
	// CloseRead reads the client's control frames, and ends ctx when the client closes or sends a message.
	ctx := conn.CloseRead(r.Context())
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case e, open := <-sub.Events():
			if !open {
				status, reason := websocket.StatusGoingAway, sub.Err().Error()
				if errors.Is(sub.Err(), ErrSubscriberTooSlow) {
					status = websocket.StatusPolicyViolation
				}
				_ = conn.Close(status, reason)
				return nil
			}
			if err := writeEvent(ctx, conn, e); err != nil {
				return nil
			}
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, updateWriteTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return nil
			}
		}
	}
}

func writeEvent(ctx context.Context, conn *websocket.Conn, e StatusEvent) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, updateWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}

// parseStatusFilter reads the notifications (notificationId, repeated) and the batch (batchId) to follow, at least
// one of them, reporting every bad parameter at once.
func parseStatusFilter(q url.Values) (StatusFilter, error) {
	var f StatusFilter
	var problems []httpx.FieldProblem
	bad := func(parameter, detail string) {
		problems = append(problems, httpx.FieldProblem{Parameter: parameter, Code: string(CodeInvalid), Detail: detail})
	}
	for _, name := range slices.Sorted(maps.Keys(q)) {
		values := q[name]
		switch name {
		case "notificationId":
			if len(values) > maxSubscribedNotifications {
				bad(name, "may be given at most "+strconv.Itoa(maxSubscribedNotifications)+" times")
				continue
			}
			for _, v := range values {
				if id, err := uuid.Parse(v); err == nil {
					f.NotificationIDs = append(f.NotificationIDs, id)
				} else {
					bad(name, "must be a UUID")
				}
			}
		case "batchId":
			if len(values) > 1 {
				bad(name, "must be given once")
			} else if id, err := uuid.Parse(values[0]); err == nil {
				f.BatchID = &id
			} else {
				bad(name, "must be a UUID")
			}
		default:
			bad(name, "is not a parameter of status updates")
		}
	}
	if len(problems) == 0 && len(f.NotificationIDs) == 0 && f.BatchID == nil {
		bad("notificationId", "give notificationId, batchId or both")
	}
	if len(problems) > 0 {
		p := httpx.NewProblem(http.StatusBadRequest, "invalid_query", "the query is invalid")
		p.Errors = problems
		return StatusFilter{}, p
	}
	return f, nil
}

// hasToken reports whether the comma-separated header holds token, in any case.
func hasToken(h http.Header, name, token string) bool {
	for _, value := range h.Values(name) {
		for t := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// sameOrigin reports whether a browser's request comes from a page of the API's own origin; a request without an
// Origin, from a program rather than a page, is allowed. A page elsewhere could otherwise use a visitor's browser to
// follow notifications.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}
