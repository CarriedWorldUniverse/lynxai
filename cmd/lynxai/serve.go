package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/CarriedWorldUniverse/lynxai/internal/api"
	"github.com/CarriedWorldUniverse/lynxai/internal/bridlecfg"
	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7878", "address to bind")
	dataDir := fs.String("data-dir", defaultDataDir(), "directory for master.key and lynxai.db")
	bridleCfg := fs.String("bridle-config", os.Getenv("LYNXAI_BRIDLE_CONFIG"), "path to bridle config (empty => synthesize default from LYNXAI_LLM_API_KEY)")
	_ = fs.Parse(args)

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	masterKey, err := creds.LoadOrCreateMasterKey(filepath.Join(*dataDir, "master.key"))
	if err != nil {
		return err
	}
	vault, err := creds.OpenVault(filepath.Join(*dataDir, "lynxai.db"), masterKey)
	if err != nil {
		return err
	}
	defer vault.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		return err
	}
	defer eng.Close()

	cfg, err := bridlecfg.Synthesize(*bridleCfg)
	if err != nil {
		return fmt.Errorf("bridle config: %w", err)
	}
	harness, err := bridlecfg.NewHarness(cfg)
	if err != nil {
		return err
	}

	router := api.NewRouter(api.Deps{
		Vault:     vault,
		Engine:    eng,
		Extractor: extract.NewExtractor(harness),
		Forms:     engine.NewFormLoginCache(),
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("lynxai serving on http://%s (data-dir=%s, llm=%s/%s)", *addr, *dataDir, cfg.Provider, cfg.Model)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func defaultDataDir() string {
	if v := os.Getenv("LYNXAI_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".lynxai"
	}
	return filepath.Join(home, ".lynxai")
}
