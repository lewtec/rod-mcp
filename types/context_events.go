package types

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// ConsoleMessage represents a captured browser console message.
type ConsoleMessage struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// NetworkRequest represents a captured network request with its response.
type NetworkRequest struct {
	RequestID string `json:"requestId"`
	Method    string `json:"method"`
	URL       string `json:"url"`
	Status    int    `json:"status"`
	Type      string `json:"type"`
}

// WebSocketConnection represents a tracked WebSocket connection.
type WebSocketConnection struct {
	RequestID     string `json:"requestId"`
	URL           string `json:"url"`
	Closed        bool   `json:"closed"`
	SentCount     int    `json:"sentCount"`
	ReceivedCount int    `json:"receivedCount"`
}

// WebSocketFrame represents a single captured WebSocket message.
type WebSocketFrame struct {
	URL         string `json:"url"`
	Direction   string `json:"direction"` // "sent" or "received"
	PayloadData string `json:"payloadData"`
	Opcode      int    `json:"opcode"`
}

// attachEventListeners registers goroutine-based listeners for console messages,
// network requests, and dialog auto-handling on a cancelable page copy. It
// returns a cancel function that stops all listener goroutines; callers must
// invoke it when the page is no longer needed to avoid goroutine leaks.
func (ctx *Context) attachEventListeners(page *rod.Page) (cancel func()) {
	// Create a cancelable copy of the page so that cancelling stops EachEvent.
	cancelPage, cancelFn := page.WithCancel()

	go cancelPage.EachEvent(func(e *proto.PageJavascriptDialogOpening) {
		// Auto-accept beforeunload dialogs to prevent navigation from hanging.
		// There is no interactive user to respond, so blocking is never useful.
		// Other dialog types (alert, confirm, prompt) are left for the
		// rod_handle_dialog tool so callers retain explicit control.
		if e.Type == proto.PageDialogTypeBeforeunload {
			if err := (proto.PageHandleJavaScriptDialog{Accept: true}).Call(page); err != nil {
				slog.Warn(fmt.Sprintf("auto-accept beforeunload dialog: %s", err))
			}
		}
	}, func(e *proto.RuntimeConsoleAPICalled) {
		var parts []string
		for _, arg := range e.Args {
			parts = append(parts, arg.Value.String())
		}
		text := strings.Join(parts, " ")
		ctx.eventsLock.Lock()
		ctx.events.AddConsoleMessage(ConsoleMessage{
			Level: string(e.Type),
			Text:  text,
		})
		ctx.eventsLock.Unlock()
	}, func(e *proto.NetworkRequestWillBeSent) {
		ctx.eventsLock.Lock()
		ctx.events.AddNetworkRequest(NetworkRequest{
			RequestID: string(e.RequestID),
			Method:    e.Request.Method,
			URL:       e.Request.URL,
			Type:      string(e.Type),
		})
		ctx.eventsLock.Unlock()
	}, func(e *proto.NetworkResponseReceived) {
		ctx.eventsLock.Lock()
		ctx.events.CompleteNetworkRequest(string(e.RequestID), e.Response.Status, string(e.Type))
		ctx.eventsLock.Unlock()
	}, func(e *proto.NetworkWebSocketCreated) {
		ctx.eventsLock.Lock()
		ctx.events.AddWSConnection(WebSocketConnection{
			RequestID: string(e.RequestID),
			URL:       e.URL,
		})
		ctx.eventsLock.Unlock()
	}, func(e *proto.NetworkWebSocketFrameSent) {
		ctx.eventsLock.Lock()
		ctx.events.UpdateWSConnection(string(e.RequestID), func(conn *WebSocketConnection) {
			conn.SentCount++
			ctx.events.AppendWSFrame(conn.URL, "sent", e.Response)
		})
		ctx.eventsLock.Unlock()
	}, func(e *proto.NetworkWebSocketFrameReceived) {
		ctx.eventsLock.Lock()
		ctx.events.UpdateWSConnection(string(e.RequestID), func(conn *WebSocketConnection) {
			conn.ReceivedCount++
			ctx.events.AppendWSFrame(conn.URL, "received", e.Response)
		})
		ctx.eventsLock.Unlock()
	}, func(e *proto.NetworkWebSocketClosed) {
		ctx.eventsLock.Lock()
		ctx.events.UpdateWSConnection(string(e.RequestID), func(conn *WebSocketConnection) {
			conn.Closed = true
		})
		ctx.eventsLock.Unlock()
	})()

	return cancelFn
}

// ConsoleMessages returns captured console messages, optionally filtered by level.
// If clear is true, the buffer is emptied after returning.
func (ctx *Context) ConsoleMessages(filterLevel string, clear bool) []ConsoleMessage {
	ctx.eventsLock.Lock()
	defer ctx.eventsLock.Unlock()
	return ctx.events.ConsoleMessages(filterLevel, clear)
}

// NetworkRequests returns captured network requests, optionally filtered by URL pattern and method.
// If clear is true, the buffer is emptied after returning.
func (ctx *Context) NetworkRequests(filterURL, filterMethod string, clear bool) []NetworkRequest {
	ctx.eventsLock.Lock()
	defer ctx.eventsLock.Unlock()
	return ctx.events.NetworkRequests(filterURL, filterMethod, clear)
}

// GetRequestID returns the CDP request ID for a network request at the given index.
func (ctx *Context) GetRequestID(index int) (string, error) {
	ctx.eventsLock.Lock()
	defer ctx.eventsLock.Unlock()
	return ctx.events.GetRequestID(index)
}

// WebSocketConnections returns tracked WebSocket connections, optionally filtered by URL.
func (ctx *Context) WebSocketConnections(urlFilter string) []WebSocketConnection {
	ctx.eventsLock.Lock()
	defer ctx.eventsLock.Unlock()
	return ctx.events.WebSocketConnections(urlFilter)
}

// WebSocketFrames returns captured WebSocket frames, optionally filtered by URL and direction.
func (ctx *Context) WebSocketFrames(urlFilter, direction string) []WebSocketFrame {
	ctx.eventsLock.Lock()
	defer ctx.eventsLock.Unlock()
	return ctx.events.WebSocketFrames(urlFilter, direction)
}

// ClearWebSocketData clears all WebSocket connections and frames.
func (ctx *Context) ClearWebSocketData() {
	ctx.eventsLock.Lock()
	defer ctx.eventsLock.Unlock()
	ctx.events.ClearWebSocketData()
}
