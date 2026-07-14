package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	ws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

var logFile *os.File

func logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] %s", ts, msg)
	fmt.Println(line)
	if logFile != nil {
		fmt.Fprintln(logFile, line)
	}
}

func main() {
	configPath := flag.String("config", "", "path to config.json")
	flag.Parse()

	if *configPath == "" {
		*configPath = os.Getenv("CONFIG_PATH")
	}
	if *configPath == "" {
		cwd, _ := os.Getwd()
		*configPath = filepath.Join(cwd, "config.json")
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("FATAL: Cannot read config from %s: %v", *configPath, err)
	}

	if cfg.Paths.LogFile != "" {
		var ferr error
		logFile, ferr = os.OpenFile(cfg.Paths.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "WARN: cannot open log file %s: %v\n", cfg.Paths.LogFile, ferr)
		}
	}

	logf("============================================================")
	logf("Opencode Feishu Channel (Go)")
	logf("============================================================")
	logf("Config: %s", *configPath)
	logf("OpenCode: %s", cfg.Opencode.URL)
	logf("Model: %s/%s", cfg.Opencode.ModelProvider, cfg.Opencode.ModelID)
	logf("Session DB: %s", cfg.Paths.SessionDb)
	logf("Pending DB: %s", cfg.Paths.PendingDb)
	logf("Timeout: %dms", cfg.Bridge.RequestTimeoutMs)

	setPendingDBPath(cfg.Paths.PendingDb)

	larkClient := lark.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret,
		lark.WithLogLevel(larkcore.LogLevelInfo))
	feishu := NewFeishuClient(larkClient, cfg.Feishu.AppID, cfg.Feishu.AppSecret)

	opencode := NewOpencodeClient(cfg.Opencode.URL, cfg.Opencode.ModelProvider, cfg.Opencode.ModelID,
		time.Duration(cfg.Bridge.RequestTimeoutMs)*time.Millisecond)

	sessions := NewSessionManager(cfg.Paths.SessionDb, opencode)

	handler := NewHandler(feishu, opencode, sessions, cfg)

	dispatcher := dispatcher.NewEventDispatcher("", "")
	dispatcher.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		handler.HandleEvent(ctx, event)
		return nil
	})

	wsClient := ws.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret,
		ws.WithEventHandler(dispatcher),
		ws.WithLogLevel(larkcore.LogLevelError))

	recoverPendingDeliveries(feishu, opencode, cfg)

	ctx := context.Background()
	go func() {
		if err := wsClient.Start(ctx); err != nil {
			logf("WS client exited: %v", err)
		}
	}()

	logf("WS connected, waiting for Feishu messages...")

	sigCh := make(chan os.Signal, 3)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigCh
	logf("Received %v, shutting down...", sig)

	wsClient.Close()
	handler.Shutdown()

	if sig == syscall.SIGUSR1 {
		logf("SIGUSR1: waiting for in-flight requests (no timeout)...")
		handler.WaitForInFlight()
		logf("Drain complete, exiting")
	} else {
		logf("SIGTERM: waiting for in-flight (timeout: %dms)...", cfg.Bridge.RequestTimeoutMs)
		done := make(chan struct{})
		go func() {
			handler.WaitForInFlight()
			close(done)
		}()
		select {
		case <-done:
			logf("All in-flight completed, exiting")
		case <-time.After(time.Duration(cfg.Bridge.RequestTimeoutMs) * time.Millisecond):
			logf("Timeout waiting, exiting")
		}
	}

	if logFile != nil {
		logFile.Close()
	}
}
