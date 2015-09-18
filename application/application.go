package application

import (
	"crypto/rand"
	"errors"
	"log"
	"os"
	"path"
	"time"

	"github.com/cloudfoundry-incubator/notifications/metrics"
	"github.com/cloudfoundry-incubator/notifications/postal"
	"github.com/cloudfoundry-incubator/notifications/strategy"
	"github.com/cloudfoundry-incubator/notifications/uaa"
	"github.com/cloudfoundry-incubator/notifications/v1/models"
	v2models "github.com/cloudfoundry-incubator/notifications/v2/models"
	"github.com/cloudfoundry-incubator/notifications/v2/util"
	"github.com/cloudfoundry-incubator/notifications/web"
	"github.com/pivotal-golang/conceal"
	"github.com/pivotal-golang/lager"
	"github.com/ryanmoran/viron"
)

const WorkerCount = 10

type Application struct {
	env      Environment
	mother   *Mother
	migrator Migrator
}

func NewApplication(mother *Mother) Application {
	env := NewEnvironment()
	databaseMigrator := models.DatabaseMigrator{}
	return Application{
		env:      env,
		mother:   mother,
		migrator: NewMigrator(mother, databaseMigrator, env.VCAPApplication.InstanceIndex == 0, env.ModelMigrationsPath, env.GobbleMigrationsPath, path.Join(env.RootPath, "templates", "default.json")),
	}
}

func (app Application) Boot() {
	session := app.mother.Logger().Session("boot")

	app.PrintConfiguration(session)
	app.ConfigureSMTP(session)
	app.RetrieveUAAPublicKey(session)
	app.migrator.Migrate()
	app.StartQueueGauge()
	app.StartWorkers()
	app.StartMessageGC()
	app.StartServer(session)
}

func (app Application) PrintConfiguration(logger lager.Logger) {
	viron.Print(app.env, vironCompatibleLogger{logger})
}

func (app Application) ConfigureSMTP(logger lager.Logger) {
	if app.env.TestMode {
		return
	}

	mailClient := app.mother.MailClient()
	err := mailClient.Connect(logger)
	if err != nil {
		logger.Fatal("smtp-connect-errored", err)
	}

	err = mailClient.Hello()
	if err != nil {
		logger.Fatal("smtp-hello-errored", err)
	}

	startTLSSupported, _ := mailClient.Extension("STARTTLS")

	mailClient.Quit()

	if !startTLSSupported && app.env.SMTPTLS {
		logger.Fatal("smtp-config-mismatch", errors.New(`SMTP TLS configuration mismatch: Configured to use TLS over SMTP, but the mail server does not support the "STARTTLS" extension.`))
	}

	if startTLSSupported && !app.env.SMTPTLS {
		logger.Fatal("smtp-config-mismatch", errors.New(`SMTP TLS configuration mismatch: Not configured to use TLS over SMTP, but the mail server does support the "STARTTLS" extension.`))
	}
}

func (app Application) RetrieveUAAPublicKey(logger lager.Logger) {
	zonedUAAClient := uaa.NewZonedUAAClient(app.env.UAAClientID, app.env.UAAClientSecret, app.env.VerifySSL, "")

	key, err := zonedUAAClient.GetTokenKey(app.env.UAAHost)
	if err != nil {
		logger.Fatal("uaa-get-token-key-errored", err)
	}

	UAAPublicKey = key
	logger.Info("uaa-public-key", lager.Data{
		"key": UAAPublicKey,
	})
}

func (app Application) StartQueueGauge() {
	if app.env.VCAPApplication.InstanceIndex != 0 {
		return
	}

	queueGauge := metrics.NewQueueGauge(app.mother.Queue(), metrics.DefaultLogger, time.Tick(1*time.Second))
	go queueGauge.Run()
}

func (app Application) StartWorkers() {
	zonedUAAClient := uaa.NewZonedUAAClient(app.env.UAAClientID, app.env.UAAClientSecret, app.env.VerifySSL, UAAPublicKey)

	WorkerGenerator{
		InstanceIndex: app.env.VCAPApplication.InstanceIndex,
		Count:         WorkerCount,
	}.Work(func(i int) Worker {
		cloak, err := conceal.NewCloak(app.env.EncryptionKey)
		if err != nil {
			panic(err)
		}
		v1Workflow := postal.NewV1Process(postal.V1ProcessConfig{
			DBTrace: app.env.DBLoggingEnabled,
			UAAHost: app.env.UAAHost,
			Sender:  app.env.Sender,
			Domain:  app.env.Domain,

			Packager:    postal.NewPackager(app.mother.TemplatesLoader(), cloak),
			MailClient:  app.mother.MailClient(),
			Database:    app.mother.Database(),
			TokenLoader: uaa.NewTokenLoader(zonedUAAClient),
			UserLoader:  postal.NewUserLoader(zonedUAAClient),

			KindsRepo:              app.mother.KindsRepo(),
			ReceiptsRepo:           app.mother.ReceiptsRepo(),
			UnsubscribesRepo:       app.mother.UnsubscribesRepo(),
			GlobalUnsubscribesRepo: app.mother.GlobalUnsubscribesRepo(),
			MessageStatusUpdater:   postal.NewMessageStatusUpdater(app.mother.MessagesRepo()),
			DeliveryFailureHandler: postal.NewDeliveryFailureHandler(),
		})

		database := v2models.NewDatabase(app.mother.SQLDatabase(), v2models.Config{})
		messageStatusUpdater := postal.NewV2MessageStatusUpdater(v2models.NewMessagesRepository(util.NewClock()))
		guidGenerator := v2models.NewGUIDGenerator(rand.Reader)
		unsubscribersRepository := v2models.NewUnsubscribersRepository(guidGenerator.Generate)
		campaignsRepository := v2models.NewCampaignsRepository(guidGenerator.Generate)

		v2Workflow := postal.NewV2Workflow(app.mother.MailClient(), postal.NewPackager(app.mother.TemplatesLoader(), cloak),
			postal.NewUserLoader(zonedUAAClient), uaa.NewTokenLoader(zonedUAAClient), messageStatusUpdater, database,
			unsubscribersRepository, campaignsRepository, app.env.Sender, app.env.Domain, app.env.UAAHost)

		worker := postal.NewDeliveryWorker(v1Workflow, v2Workflow, postal.DeliveryWorkerConfig{
			ID:                     i,
			UAAHost:                app.env.UAAHost,
			Logger:                 app.mother.Logger(),
			Queue:                  app.mother.Queue(),
			DBTrace:                app.env.DBLoggingEnabled,
			Database:               database,
			StrategyDeterminer:     strategy.NewStrategyDeterminer(app.mother.UserStrategy(), app.mother.SpaceStrategy(), app.mother.OrganizationStrategy(), app.mother.EmailStrategy()),
			DeliveryFailureHandler: postal.NewDeliveryFailureHandler(),
			MessageStatusUpdater:   messageStatusUpdater,
		})
		return &worker
	})
}

func (app Application) StartMessageGC() {
	messageLifetime := 24 * time.Hour
	db := app.mother.Database()
	messagesRepo := app.mother.MessagesRepo()
	pollingInterval := 1 * time.Hour

	logger := log.New(os.Stdout, "", 0)
	messageGC := postal.NewMessageGC(messageLifetime, db, messagesRepo, pollingInterval, logger)
	messageGC.Run()
}

func (app Application) StartServer(logger lager.Logger) {
	web.NewServer().Run(app.mother, web.Config{
		DBLoggingEnabled: app.env.DBLoggingEnabled,
		SkipVerifySSL:    !app.env.VerifySSL,
		Port:             app.env.Port,
		Logger:           logger,
		CORSOrigin:       app.env.CORSOrigin,
		SQLDB:            app.mother.SQLDatabase(),

		UAAPublicKey:    UAAPublicKey,
		UAAHost:         app.env.UAAHost,
		UAAClientID:     app.env.UAAClientID,
		UAAClientSecret: app.env.UAAClientSecret,
		CCHost:          app.env.CCHost,
	})
}

// This is a hack to get the logs output to the loggregator before the process exits
func (app Application) Crash() {
	logger := app.mother.Logger()

	err := recover()
	switch err.(type) {
	case error:
		time.Sleep(5 * time.Second)
		logger.Fatal("crash", err.(error))
	case nil:
		return
	default:
		time.Sleep(5 * time.Second)
		logger.Fatal("crash", nil)
	}
}

type vironCompatibleLogger struct {
	logger lager.Logger
}

func (l vironCompatibleLogger) Printf(format string, v ...interface{}) {
	if len(v) == 2 {
		key, ok := v[0].(string)
		value := v[1]
		if ok {
			l.logger.Info("viron", lager.Data{key: value})
		}
	}
}
