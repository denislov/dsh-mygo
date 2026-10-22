// Command spike validates the P0 host bridge: spawn a DeepSeek Harness SDK
// profile, initialize it, send one prompt, and print committed session events.
// It is intentionally a console program; the MyGo UI replaces it in P1.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, " ") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spike: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var hostArgs stringList
	flag.Var(&hostArgs, "host-arg", "argument for -host-command (repeatable)")

	dshDir := flag.String("dsh-dir", os.Getenv("DSH_MYGO_DSH_DIR"), "DeepSeek Harness checkout; runs apps/cli/src/bin.ts through tsx")
	profile := flag.String("profile", "sdk", "DSH profile")
	provider := flag.String("provider", "deepseek-official", "model provider")
	model := flag.String("model", "deepseek-flash", "model id")
	effort := flag.String("effort", "", "optional reasoning effort")
	cwd := flag.String("cwd", "", "session working directory (default: current directory)")
	prompt := flag.String("prompt", "Say hello in one sentence.", "user prompt")
	sessionID := flag.String("session", "", "session id (default: generated)")
	dshHome := flag.String("dsh-home", "", "optional DSH_HOME override")
	timeout := flag.Duration("timeout", 5*time.Minute, "whole-turn deadline")
	maxFrame := flag.Int("max-frame", channel.DefaultMaxFrameBytes, "maximum JSON-RPC frame bytes")
	hostCommand := flag.String("host-command", "", "override the host executable")
	flag.Parse()

	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("prompt is empty")
	}
	workingDirectory := *cwd
	if workingDirectory == "" {
		current, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
		workingDirectory = current
	}
	absoluteCwd, err := filepath.Abs(workingDirectory)
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	if *sessionID == "" {
		*sessionID = fmt.Sprintf("spike-%d", time.Now().UnixNano())
	}

	config, err := host.ResolveLaunch(host.LaunchOptions{
		DshDir:  *dshDir,
		Profile: *profile,
		CWD:     absoluteCwd,
		Command: *hostCommand,
		Args:    hostArgs,
		DSHHome: *dshHome,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	supervisor, err := host.Start(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = supervisor.Stop(stopCtx)
		if tail := strings.TrimSpace(supervisor.StderrTail()); tail != "" {
			fmt.Fprintf(os.Stderr, "\n--- host stderr (tail) ---\n%s\n", tail)
		}
	}()

	client := channel.NewClient(supervisor.Stdout(), supervisor.Stdin(), *maxFrame)
	var promptSent atomic.Bool
	var sawTurnActivity atomic.Bool
	turnDone := make(chan struct{})
	var turnOnce sync.Once
	client.OnNotification(func(method string, params json.RawMessage) {
		printNotification(method, params)
		switch method {
		case "session.event":
			if isTurnActivity(params, *sessionID) {
				sawTurnActivity.Store(true)
			}
		case "session.status":
			if promptSent.Load() && sawTurnActivity.Load() && isIdle(params, *sessionID) {
				turnOnce.Do(func() { close(turnDone) })
			}
		}
	})
	go func() {
		if err := client.Run(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, channel.ErrClosed) {
			fmt.Fprintf(os.Stderr, "spike: read loop: %v\n", err)
		}
	}()
	defer client.Close()

	initialize := map[string]any{
		"cwd":      absoluteCwd,
		"provider": *provider,
		"model":    *model,
	}
	if *effort != "" {
		initialize["reasoningEffort"] = *effort
	}
	initialized, err := client.Call(ctx, "initialize", initialize)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Printf("[initialize] %s\n", truncate(string(initialized), 1000))
	fmt.Printf("[session] %s\n[prompt] %s\n", *sessionID, *prompt)

	promptSent.Store(true)
	promptResult, err := client.Call(ctx, "session/prompt", map[string]any{
		"sessionId": *sessionID,
		"contentBlocks": []map[string]any{
			{"type": "text", "text": *prompt},
		},
	})
	if err != nil {
		return fmt.Errorf("session/prompt: %w", err)
	}
	fmt.Printf("[prompt accepted] %s\n", truncate(string(promptResult), 500))

	// The SDK server does not acknowledge a turn with session/wait before the
	// queued prompt is claimed; follow the official client: wait for the user
	// message receipt, then for the session to become idle.
	select {
	case <-turnDone:
	case <-ctx.Done():
		return fmt.Errorf("wait for turn: %w", ctx.Err())
	}
	fmt.Println("[turn complete]")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if _, err := client.Call(shutdownCtx, "shutdown", map[string]any{}); err != nil {
		fmt.Fprintf(os.Stderr, "spike: shutdown: %v\n", err)
	}
	return nil
}

func printNotification(method string, params json.RawMessage) {
	switch method {
	case "session.event":
		var notification struct {
			SessionID string `json:"sessionId"`
			Event     struct {
				Type string          `json:"type"`
				Seq  uint64          `json:"seq"`
				Data json.RawMessage `json:"data"`
			} `json:"event"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			fmt.Printf("[notify] session.event (unreadable: %v)\n", err)
			return
		}
		fmt.Printf("[event %d] %s\n", notification.Event.Seq, notification.Event.Type)
		printEventData(notification.Event.Type, notification.Event.Data)
	case "session.status":
		var notification struct {
			SessionID string `json:"sessionId"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal(params, &notification); err == nil {
			fmt.Printf("[status] %s\n", notification.Status)
		}
	default:
		fmt.Printf("[notify] %s %s\n", method, truncate(string(params), 500))
	}
}

func printEventData(eventType string, data json.RawMessage) {
	switch eventType {
	case "user/message":
		var message struct {
			Content []textBlock `json:"content"`
		}
		if err := json.Unmarshal(data, &message); err == nil {
			printBlocks("user", message.Content)
		}
	case "assistant/message":
		var event struct {
			Message struct {
				Content []textBlock `json:"content"`
			} `json:"message"`
			Interrupted bool `json:"interrupted"`
		}
		if err := json.Unmarshal(data, &event); err == nil {
			printBlocks("assistant", event.Message.Content)
			if event.Interrupted {
				fmt.Println("  [interrupted]")
			}
		}
	case "tool/call":
		var call struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if err := json.Unmarshal(data, &call); err == nil {
			fmt.Printf("  [tool] %s %s\n", call.Name, truncate(call.Arguments, 500))
		}
	case "tool/result":
		var result struct {
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(data, &result); err == nil {
			fmt.Printf("  [tool result] error=%t\n", result.IsError)
		}
	default:
		fmt.Printf("  %s\n", truncate(string(data), 500))
	}
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func printBlocks(label string, blocks []textBlock) {
	var parts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		case "reasoning":
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, "[reasoning] "+block.Text)
			}
		case "tool-call":
			parts = append(parts, "[tool-call block]")
		}
	}
	if len(parts) == 0 {
		return
	}
	fmt.Printf("  %s: %s\n", label, truncate(strings.Join(parts, "\n"), 4000))
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max]) + "…"
}

// isTurnActivity reports whether the notification proves the queued prompt
// started a turn. An idle status emitted before the prompt is claimed must not
// end the spike; the SDK emits synthetic user-role context too, and any of
// these events means the turn is live.
func isTurnActivity(params json.RawMessage, sessionID string) bool {
	var notification struct {
		SessionID string `json:"sessionId"`
		Event     struct {
			Type string `json:"type"`
		} `json:"event"`
	}
	if err := json.Unmarshal(params, &notification); err != nil {
		return false
	}
	if notification.SessionID != sessionID {
		return false
	}
	switch notification.Event.Type {
	case "turn/start", "step/start", "user/message", "assistant/message", "tool/call", "turn/end":
		return true
	default:
		return false
	}
}

func isIdle(params json.RawMessage, sessionID string) bool {
	var notification struct {
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(params, &notification); err != nil {
		return false
	}
	return notification.SessionID == sessionID && notification.Status == "idle"
}
