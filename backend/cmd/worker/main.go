package main

import (
	"context"
	attachmentsrepo "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/repository"
	attachmentsusecase "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/furkanmeclis/traccia/backend/internal/config"
	"github.com/furkanmeclis/traccia/backend/internal/database"
	"github.com/furkanmeclis/traccia/backend/internal/logging"
	bulkusecase "github.com/furkanmeclis/traccia/backend/internal/modules/bulk/usecase"
	exportusecase "github.com/furkanmeclis/traccia/backend/internal/modules/exports/usecase"
	importusecase "github.com/furkanmeclis/traccia/backend/internal/modules/imports/usecase"
	logsusecase "github.com/furkanmeclis/traccia/backend/internal/modules/logs/usecase"
	notifmodule "github.com/furkanmeclis/traccia/backend/internal/modules/notifications"
	"github.com/furkanmeclis/traccia/backend/internal/modules/notifications/providers"
	notifusecase "github.com/furkanmeclis/traccia/backend/internal/modules/notifications/usecase"
	workspacesrepo "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	"github.com/furkanmeclis/traccia/backend/internal/observability"
	"github.com/furkanmeclis/traccia/backend/internal/platform/activity"
	"github.com/furkanmeclis/traccia/backend/internal/platform/bulkengine"
	bulkadapters "github.com/furkanmeclis/traccia/backend/internal/platform/bulkengine/adapters"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/ioengine"
	ioadapters "github.com/furkanmeclis/traccia/backend/internal/platform/ioengine/adapters"
	"github.com/furkanmeclis/traccia/backend/internal/platform/mail"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/furkanmeclis/traccia/backend/internal/platform/searchengine"
	searchadapters "github.com/furkanmeclis/traccia/backend/internal/platform/searchengine/adapters"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"github.com/furkanmeclis/traccia/backend/internal/queue"
	"github.com/furkanmeclis/traccia/backend/internal/realtime"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		_, _ = os.Stderr.WriteString("config load failed: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	flushSentry, err := observability.Init(cfg.Sentry, "worker")
	if err != nil {
		log.Warn("sentry_init_failed", "error", err)
	} else if observability.Enabled() {
		log.Info("sentry_enabled", "environment", cfg.Sentry.Environment, "traces_sample_rate", cfg.Sentry.TracesSampleRate)
	}
	defer flushSentry()
	// exit flushes pending error events before leaving (os.Exit skips defers).
	exit := func(code int) {
		flushSentry()
		os.Exit(code)
	}
	log = slog.New(observability.NewSlogHandler(log.Handler()))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPostgresPool(ctx, cfg.DB)
	if err != nil {
		log.Error("database_init_failed", "error", err)
		exit(1)
	}
	defer pool.Close()

	queries := database.NewQueries(pool)
	durableRegistry, err := workspacesrepo.New(pool, queries).DurableConsumers()
	if err != nil {
		log.Error("delivery_registry_failed", "error", err)
		return
	}
	durableCtx, cancelDurable := context.WithCancel(ctx)
	durableDone := make(chan struct{})
	go func() {
		defer close(durableDone)
		outbox.NewWorker(outbox.NewStore(pool, queries), durableRegistry, log).Run(durableCtx)
	}()
	defer func() { cancelDurable(); <-durableDone }()
	store, err := storage.NewFromConfig(ctx, cfg.Storage)
	if err != nil {
		log.Error("storage_init_failed", "error", err)
		exit(1)
	}

	attachmentStore, err := attachmentsrepo.NewObjectStore(store)
	if err != nil {
		log.Error("attachment_storage_init_failed")
		return
	}
	attachmentOptions := attachmentsusecase.Options{MaxFileBytes: cfg.Attachments.MaxFileBytes, ChunkBytes: cfg.Attachments.ChunkBytes, UploadTTL: cfg.Attachments.UploadTTL, FinalizedRetention: cfg.Attachments.FinalizedRetention, CleanupInterval: cfg.Attachments.CleanupInterval}
	attachmentRepo, err := attachmentsrepo.New(pool, queries, attachmentStore, attachmentOptions)
	if err != nil {
		log.Error("attachment_init_failed")
		return
	}
	attachmentCtx, cancelAttachments := context.WithCancel(ctx)
	attachmentDone := make(chan struct{})
	go func() {
		defer close(attachmentDone)
		attachmentRepo.RunCleanup(attachmentCtx, func(error) { log.Error("attachment_cleanup_failed") })
	}()
	defer func() { cancelAttachments(); <-attachmentDone }()
	if !cfg.Queue.Enabled {
		log.Info("durable_worker_started", "redis_queue_enabled", false)
		<-ctx.Done()
		return
	}

	var publisher realtime.Publisher
	if cfg.Centrifugo.Enabled {
		publisher = realtime.NewCentrifugoClient(cfg.Centrifugo)
	} else {
		publisher = realtime.NoopPublisher{}
	}

	mailer := mail.NewSMTPSender(cfg.SMTP)
	provs := []providers.Provider{
		providers.InappProvider{},
		providers.EmailProvider{Mail: mailer},
		providers.RealtimeProvider{Pub: publisher},
		providers.NoopProvider{Name: "sms", Log: log},
		providers.NoopProvider{Name: "push", Log: log},
	}
	notifSvc := notifusecase.New(queries, nil, provs, log)
	notifSvc.WithVAPID(notifusecase.VAPIDConfig{
		PublicKey:  cfg.VAPID.PublicKey,
		PrivateKey: cfg.VAPID.PrivateKey,
		Subject:    cfg.VAPID.Subject,
	})
	notifSvc.WithExpo(notifusecase.ExpoConfig{AccessToken: cfg.Expo.AccessToken})

	eventBus := events.NewBus(log)
	notifmodule.RegisterEventHandlers(eventBus, notifSvc, log)

	activityRec := activity.NewRecorder(queries, log)
	ioReg := ioengine.NewRegistry(
		ioadapters.NewUsers(queries),
		ioadapters.NewRoles(queries),
		ioadapters.NewNotifications(queries),
		ioadapters.NewActivity(queries),
	)
	exportSvc := exportusecase.New(queries, store, ioReg, nil, notifSvc, activityRec, log)
	importSvc := importusecase.New(queries, store, ioReg, nil, notifSvc, activityRec, log)
	bulkReg := bulkengine.NewRegistry(
		bulkadapters.NewUsers(queries),
		bulkadapters.NewRoles(queries),
	)
	bulkSvc := bulkusecase.New(queries, bulkReg, nil, notifSvc, activityRec, cfg.Bulk, log)
	logsSvc := logsusecase.New(queries)
	searchReg := searchengine.NewRegistry(
		searchadapters.NewUsers(queries),
		searchadapters.NewRoles(queries),
	)
	searchClient := searchengine.NewClient(cfg.Search, log)
	searchIndexer := searchengine.NewIndexer(searchClient, searchReg, nil, log)
	// Deliveries run inline here; mobile pushes go through the queue (retries).
	queueClient := queue.NewClient(cfg.Redis)
	defer func() { _ = queueClient.Close() }()
	notifSvc.WithPushQueue(queueClient)
	persist := logging.Attach(log, logsSvc)
	log = persist.Logger()
	defer persist.Close()

	worker := queue.NewWorker(cfg, log, notifSvc.Deliver).
		WithExport(exportSvc.ProcessExport).
		WithImport(importSvc.ProcessImport).
		WithBulk(bulkSvc.ProcessBulk).
		WithLogPurge(logsSvc.ApplyDueRules).
		WithMobilePush(notifSvc.SendMobilePush, notifSvc.ProcessPushReceipts).
		WithSearch(
			searchIndexer.ProcessUpsert,
			searchIndexer.ProcessDelete,
			searchIndexer.ProcessReindex,
		)

	if searchIndexer.Enabled() {
		go searchIndexer.Bootstrap(ctx)
	}

	scheduler, err := queue.StartLogPurgeScheduler(cfg, log)
	if err != nil {
		log.Error("log_purge_scheduler_failed", "error", err)
		exit(1)
	}
	if err := queue.RegisterPushReceiptsSchedule(scheduler); err != nil {
		log.Error("push_receipts_scheduler_failed", "error", err)
		exit(1)
	}
	go func() {
		if err := scheduler.Run(); err != nil {
			log.Error("log_purge_scheduler_stopped", "error", err)
		}
	}()

	log.Info("worker_started",
		"app", cfg.App.Name,
		"env", cfg.App.Env,
		"concurrency", cfg.Queue.Concurrency,
	)

	if n, err := notifSvc.ReclaimStuck(ctx, notifusecase.DefaultStuckProcessingMinutes); err != nil {
		log.Error("notification_reclaim_failed", "error", err)
	} else if n > 0 {
		log.Info("notification_reclaim_completed", "count", n)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Start()
	}()

	select {
	case <-ctx.Done():
		log.Info("worker_shutdown_signal")
		worker.Shutdown()
	case err := <-errCh:
		if err != nil {
			log.Error("worker_failed", "error", err)
			exit(1)
		}
	}
	log.Info("worker_stopped")
}
