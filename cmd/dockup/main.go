package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/shui1iao/DockUP/internal/agent"
	"github.com/shui1iao/DockUP/internal/config"
	"github.com/shui1iao/DockUP/internal/dockerx"
	"github.com/shui1iao/DockUP/internal/reverse"
	"github.com/shui1iao/DockUP/internal/telegram"
	"github.com/shui1iao/DockUP/internal/updater"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config failed", "error", err)
		os.Exit(1)
	}

	docker, err := dockerx.New(log)
	if err != nil {
		log.Error("connect docker failed", "error", err)
		os.Exit(1)
	}
	defer docker.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if config.ModeFromEnv() == "pair" {
		if err := agent.Pair(ctx, cfg.PublicURL, os.Getenv("DOCKUP_PAIR_ID"), cfg.AgentToken); err != nil {
			log.Error("DockUP pair failed", "error", err)
			os.Exit(1)
		}
		log.Info("DockUP pair request sent")
		return
	}

	if config.ModeFromEnv() == "agent" {
		if cfg.PublicURL != "" {
			log.Info("DockUP booting in reverse agent mode", "version", version, "center", cfg.PublicURL)
			if err := reverse.NewAgent(docker, cfg.PublicURL, cfg.AgentToken, cfg.LocalName, log, cfg.Timeout).Run(ctx); err != nil && err != context.Canceled {
				log.Error("DockUP reverse agent stopped with error", "error", err)
				os.Exit(1)
			}
			return
		}
		log.Info("DockUP booting in agent mode", "version", version, "listen", cfg.AgentListen)
		if err := agent.NewServer(docker, cfg.AgentToken, cfg.LocalName, log).Start(ctx, cfg.AgentListen); err != nil && err != context.Canceled {
			log.Error("DockUP agent stopped with error", "error", err)
			os.Exit(1)
		}
		return
	}

	if targetID := os.Getenv("DOCKUP_APPLY_CONTAINER_ID"); targetID != "" {
		imageRef := os.Getenv("DOCKUP_APPLY_IMAGE_REF")
		log.Info("DockUP self-update helper applying update", "container", targetID, "image", imageRef)
		_, _, err := docker.UpdateContainer(ctx, targetID, imageRef, cfg.Cleanup)
		if err != nil {
			log.Error("DockUP self-update helper failed", "error", err)
			os.Exit(1)
		}
		log.Info("DockUP self-update helper finished")
		return
	}

	bot := telegram.New(cfg.TelegramBotToken, cfg.TelegramChatID)
	log.Info("DockUP booting", "version", version, "interval", cfg.CheckInterval.String(), "telegram", bot.Enabled())
	if bot.Enabled() {
		if err := bot.SetCommands(ctx); err != nil {
			log.Warn("set telegram commands failed", "error", err)
		}
	}
	if cfg.SetupTestMessage && bot.Enabled() {
		text := fmt.Sprintf("✅ DockUP 已启动 / 已更新\n\n版本：%s\n检测间隔：%s\n\n点击下面按钮进入 Docker 管理面板，可查看项目状态、手动检查更新、启动/停止/重启或二次确认删除。", version, cfg.CheckInterval.String())
		if _, err := bot.SendSetupTest(ctx, text); err != nil {
			log.Warn("setup message failed", "error", err)
		}
	}

	store, _ := config.NewStore(cfg.DataPath)
	hub := reverse.NewHub(store, log)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/reverse/connect", hub.Handle)
	srv := &http.Server{Addr: cfg.AgentListen, Handler: mux}
	go func() {
		log.Info("DockUP reverse hub listening", "listen", cfg.AgentListen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warn("reverse hub stopped", "error", err)
		}
	}()
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()

	app := updater.New(cfg, docker, bot, log, store, hub)
	if err := app.Run(ctx); err != nil && err != context.Canceled {
		log.Error("DockUP stopped with error", "error", err)
		os.Exit(1)
	}
}
