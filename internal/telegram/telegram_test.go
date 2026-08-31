package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallReturnsTelegramAPIErrorWithoutResultTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"description":"callback query is too old"}`))
	}))
	defer server.Close()

	bot := New("test-token", "123")
	bot.apiBaseURL = server.URL
	bot.client = server.Client()

	err := bot.AnswerCallback(context.Background(), "callback-id", "done")
	if err == nil || !strings.Contains(err.Error(), "callback query is too old") {
		t.Fatalf("expected Telegram API error, got %v", err)
	}
}

func TestPollCallbacksRecoversAfterTelegramError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":[{"update_id":42,"callback_query":{"id":"cb-1","data":"home","from":{"id":123},"message":{"message_id":7,"chat":{"id":123}}}}]}`)
	}))
	defer server.Close()

	bot := New("test-token", "123")
	bot.apiBaseURL = server.URL
	bot.client = server.Client()
	bot.pollRetryMin = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Callback, 1)
	done := make(chan error, 1)
	go func() { done <- bot.PollCallbacks(ctx, out) }()

	select {
	case callback := <-out:
		if callback.ID != "cb-1" || callback.Data != "home" || callback.MessageID != 7 {
			t.Fatalf("unexpected callback: %+v", callback)
		}
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("callback polling did not recover after a temporary error")
	}

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback polling did not stop after cancellation")
	}
}
