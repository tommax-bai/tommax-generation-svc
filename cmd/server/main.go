// generation-svc 入口。-mode=api|worker|all（默认 all，本地开发单进程跑两面）。
// Phase 1 手动装配（wire 为规模化后统一项，见 README 例外登记）。
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/tommax-bai/tommax-go-kit/configx"
	"github.com/tommax-bai/tommax-go-kit/httpx"
	"github.com/tommax-bai/tommax-go-kit/logx"
	"github.com/tommax-bai/tommax-go-kit/objstore"

	"github.com/tommax-bai/tommax-generation-svc/internal/conf"
	"github.com/tommax-bai/tommax-generation-svc/internal/handler"
	"github.com/tommax-bai/tommax-generation-svc/internal/repo"
	"github.com/tommax-bai/tommax-generation-svc/internal/service"
	"github.com/tommax-bai/tommax-generation-svc/internal/worker"
	"github.com/tommax-bai/tommax-generation-svc/migrations"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "config file path")
	mode := flag.String("mode", "all", "api | worker | all")
	flag.Parse()

	var cfg conf.Config
	if err := configx.Load(*configPath, &cfg); err != nil {
		panic(err)
	}
	log := logx.Init("generation-svc", cfg.Log.Level, cfg.Log.Format)

	if err := runMigrations(cfg.DB.DSN); err != nil {
		log.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DB.DSN)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store, err := objstore.New(cfg.ObjStore)
	if err != nil {
		log.Error("objstore init failed", "err", err)
		os.Exit(1)
	}
	catalog, err := repo.NewCatalogFile(cfg.Catalog.Path)
	if err != nil {
		log.Error("catalog load failed", "err", err)
		os.Exit(1)
	}

	taskRepo := repo.NewTaskPG(pool)
	queue := repo.NewQueueAsynq(cfg.Redis.Addr, cfg.Worker.MaxRetry,
		time.Duration(cfg.Worker.JobTimeoutSec+60)*time.Second)
	defer queue.Close()
	svc := service.NewGenerationService(taskRepo, catalog, queue)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	var httpServer *http.Server
	var asynqServer *asynq.Server

	if *mode == "api" || *mode == "all" {
		r := chi.NewRouter()
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		r.Group(func(protected chi.Router) {
			protected.Use(func(next http.Handler) http.Handler { return httpx.Chain(next, httpx.DevAuth) })
			handler.NewGenerationHandler(svc).Mount(protected)
		})
		httpServer = &http.Server{Addr: cfg.Server.HTTPAddr, Handler: r}
		go func() {
			log.Info("http listening", "addr", cfg.Server.HTTPAddr)
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("http serve exit", "err", err)
				os.Exit(1)
			}
		}()
		log.Warn("DevAuth enabled — dev only, replace with Casdoor JWT before any non-local deployment")
	}

	if *mode == "worker" || *mode == "all" {
		adapter, err := worker.NewAdapterClient(cfg.Adapter.Addr)
		if err != nil {
			log.Error("adapter client init failed", "err", err)
			os.Exit(1)
		}
		h := worker.NewHandler(taskRepo, catalog, adapter, store,
			time.Duration(cfg.Worker.PollIntervalMs)*time.Millisecond,
			time.Duration(cfg.Worker.JobTimeoutSec)*time.Second)
		asynqServer = asynq.NewServer(
			asynq.RedisClientOpt{Addr: cfg.Redis.Addr},
			asynq.Config{
				Concurrency: cfg.Worker.Concurrency,
				Queues:      map[string]int{repo.QueueDispatch: 10},
			},
		)
		mux := asynq.NewServeMux()
		mux.HandleFunc(repo.TypeDispatch, h.HandleDispatch)
		go func() {
			log.Info("worker started", "concurrency", cfg.Worker.Concurrency)
			if err := asynqServer.Run(mux); err != nil {
				log.Error("worker exit", "err", err)
				os.Exit(1)
			}
		}()
	}

	<-stop
	log.Info("shutting down")
	if asynqServer != nil {
		asynqServer.Shutdown() // 完成在手任务再退（docs/03 §1.3）
	}
	if httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}
}

func runMigrations(dsn string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer db.Close()
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
