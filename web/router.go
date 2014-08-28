package web

import (
    "net"
    "strings"

    "github.com/cloudfoundry-incubator/notifications/config"
    "github.com/cloudfoundry-incubator/notifications/mail"
    "github.com/cloudfoundry-incubator/notifications/models"
    "github.com/cloudfoundry-incubator/notifications/postal"
    "github.com/cloudfoundry-incubator/notifications/web/handlers"
    "github.com/gorilla/mux"
    "github.com/ryanmoran/stack"
)

const WorkerCount = 10

type Router struct {
    stacks map[string]stack.Stack
}

func NewRouter() Router {
    mother := NewMother()

    StartWorkers(mother)

    registrar := mother.Registrar()
    notify := handlers.NewNotify(mother.Courier(), mother.Finder(), registrar)
    preference := handlers.NewPreference(models.NewPreferencesRepo())
    preferenceUpdater := handlers.NewPreferenceUpdater(models.NewUnsubscribesRepo())
    logging := mother.Logging()
    errorWriter := mother.ErrorWriter()
    authenticator := mother.Authenticator()

    return Router{
        stacks: map[string]stack.Stack{
            "GET /info":               stack.NewStack(handlers.NewGetInfo()).Use(logging),
            "POST /users/{guid}":      stack.NewStack(handlers.NewNotifyUser(notify, errorWriter)).Use(logging, authenticator),
            "POST /spaces/{guid}":     stack.NewStack(handlers.NewNotifySpace(notify, errorWriter)).Use(logging, authenticator),
            "PUT /registration":       stack.NewStack(handlers.NewRegistration(registrar, errorWriter)).Use(logging, authenticator),
            "GET /user_preferences":   stack.NewStack(handlers.NewPreferenceFinder(preference, errorWriter)).Use(logging),
            "PATCH /user_preferences": stack.NewStack(handlers.NewUpdatePreferences(preferenceUpdater, errorWriter)).Use(logging),
        },
    }
}

func StartWorkers(mother *Mother) {
    env := config.NewEnvironment()
    for i := 0; i < WorkerCount; i++ {
        mailClient, err := mail.NewClient(env.SMTPUser, env.SMTPPass, net.JoinHostPort(env.SMTPHost, env.SMTPPort))
        if err != nil {
            panic(err)
        }
        mailClient.Insecure = !env.VerifySSL
        worker := postal.NewDeliveryWorker(i+1, mother.Logger(), mailClient, mother.Queue())
        worker.Work()
    }
}

func (router Router) Routes() *mux.Router {
    r := mux.NewRouter()
    for methodPath, stack := range router.stacks {
        var name = methodPath
        parts := strings.SplitN(methodPath, " ", 2)
        r.Handle(parts[1], stack).Methods(parts[0]).Name(name)
    }
    return r
}
