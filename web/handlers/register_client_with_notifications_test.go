package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/cloudfoundry-incubator/notifications/application"
	"github.com/cloudfoundry-incubator/notifications/fakes"
	"github.com/cloudfoundry-incubator/notifications/models"
	"github.com/cloudfoundry-incubator/notifications/postal"
	"github.com/cloudfoundry-incubator/notifications/web/handlers"
	"github.com/cloudfoundry-incubator/notifications/web/params"
	"github.com/dgrijalva/jwt-go"
	"github.com/ryanmoran/stack"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RegisterClientWithNotifications", func() {
	var (
		handler     handlers.RegisterClientWithNotifications
		writer      *httptest.ResponseRecorder
		request     *http.Request
		errorWriter *fakes.ErrorWriter
		conn        *fakes.Connection
		registrar   *fakes.Registrar
		client      models.Client
		kinds       []models.Kind
		context     stack.Context
	)

	BeforeEach(func() {
		conn = fakes.NewConnection()
		database := fakes.NewDatabase()
		database.Conn = conn
		errorWriter = fakes.NewErrorWriter()
		registrar = fakes.NewRegistrar()
		handler = handlers.NewRegisterClientWithNotifications(registrar, errorWriter)
		writer = httptest.NewRecorder()
		requestBody, err := json.Marshal(map[string]interface{}{
			"source_name": "Raptor Containment Unit",
			"notifications": map[string]interface{}{
				"perimeter_breach": map[string]interface{}{
					"description": "Perimeter Breach",
					"critical":    true,
				},
				"feeding_time": map[string]interface{}{
					"description": "Feeding Time",
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		request, err = http.NewRequest("PUT", "/notifications", bytes.NewBuffer(requestBody))
		Expect(err).NotTo(HaveOccurred())

		tokenHeader := map[string]interface{}{
			"alg": "FAST",
		}
		tokenClaims := map[string]interface{}{
			"client_id": "raptors",
			"exp":       int64(3404281214),
			"scope":     []string{"notifications.write", "critical_notifications.write"},
		}
		rawToken := fakes.BuildToken(tokenHeader, tokenClaims)
		request.Header.Set("Authorization", "Bearer "+rawToken)

		token, err := jwt.Parse(rawToken, func(*jwt.Token) (interface{}, error) {
			return []byte(application.UAAPublicKey), nil
		})
		Expect(err).NotTo(HaveOccurred())

		context = stack.NewContext()
		context.Set("token", token)
		context.Set("database", database)

		client = models.Client{
			ID:          "raptors",
			Description: "Raptor Containment Unit",
		}

		kinds = []models.Kind{
			{
				ID:          "perimeter_breach",
				Description: "Perimeter Breach",
				Critical:    true,
				ClientID:    client.ID,
			},
			{
				ID:          "feeding_time",
				Description: "Feeding Time",
				ClientID:    client.ID,
			},
		}
	})

	Describe("Execute", func() {
		It("passes the correct arguments to Register", func() {
			handler.ServeHTTP(writer, request, context)

			Expect(registrar.RegisterCall.Arguments.Connection).To(Equal(conn))
			Expect(registrar.RegisterCall.Arguments.Client).To(Equal(client))
			Expect(registrar.RegisterCall.Arguments.Kinds).To(ConsistOf(kinds))

			Expect(conn.BeginWasCalled).To(BeTrue())
			Expect(conn.CommitWasCalled).To(BeTrue())
			Expect(conn.RollbackWasCalled).To(BeFalse())
		})

		It("passes the correct arguments to Prune", func() {
			handler.ServeHTTP(writer, request, context)

			Expect(registrar.PruneCall.Arguments.Connection).To(Equal(conn))
			Expect(registrar.PruneCall.Arguments.Client).To(Equal(client))
			Expect(registrar.PruneCall.Arguments.Kinds).To(ConsistOf(kinds))

			Expect(conn.BeginWasCalled).To(BeTrue())
			Expect(conn.CommitWasCalled).To(BeTrue())
			Expect(conn.RollbackWasCalled).To(BeFalse())
		})

		It("does not prune kinds if they are not in the request", func() {
			requestBody, err := json.Marshal(map[string]interface{}{
				"source_name": "Raptor Containment Unit",
			})
			Expect(err).NotTo(HaveOccurred())

			request.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))

			handler.ServeHTTP(writer, request, context)

			Expect(registrar.PruneCall.Called).To(BeFalse())

			Expect(conn.BeginWasCalled).To(BeTrue())
			Expect(conn.CommitWasCalled).To(BeTrue())
			Expect(conn.RollbackWasCalled).To(BeFalse())
		})

		Context("failure cases", func() {
			It("rejects entire request and returns 404 error if notification is critical without scope", func() {
				requestBody, err := json.Marshal(map[string]interface{}{
					"source_name": "Raptor Containment Unit",
					"notifications": map[string]interface{}{
						"perimeter_breach": map[string]interface{}{
							"description": "Perimeter Breach",
							"critical":    true,
						},
						"feeding_time": map[string]interface{}{
							"description": "Feeding Time",
							"critical":    true,
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())

				request, err = http.NewRequest("PUT", "/notifications", bytes.NewBuffer(requestBody))
				Expect(err).NotTo(HaveOccurred())

				tokenHeader := map[string]interface{}{
					"alg": "FAST",
				}
				tokenClaims := map[string]interface{}{
					"client_id": "raptors",
					"exp":       int64(3404281214),
					"scope":     []string{"notifications.write"},
				}
				rawToken := fakes.BuildToken(tokenHeader, tokenClaims)
				request.Header.Set("Authorization", "Bearer "+rawToken)

				token, err := jwt.Parse(rawToken, func(*jwt.Token) (interface{}, error) {
					return []byte(application.UAAPublicKey), nil
				})
				Expect(err).NotTo(HaveOccurred())

				context.Set("token", token)

				handler.ServeHTTP(writer, request, context)
				Expect(errorWriter.Error).To(BeAssignableToTypeOf(postal.UAAScopesError("waaaaat")))

				Expect(conn.BeginWasCalled).To(BeFalse())
				Expect(conn.CommitWasCalled).To(BeFalse())
				Expect(conn.RollbackWasCalled).To(BeFalse())
			})

			It("delegates parsing errors to the ErrorWriter", func() {
				request, err := http.NewRequest("PUT", "/notifications", strings.NewReader("this is not valid JSON"))
				Expect(err).NotTo(HaveOccurred())

				handler.ServeHTTP(writer, request, context)

				Expect(errorWriter.Error).To(BeAssignableToTypeOf(params.ParseError{}))

				Expect(conn.BeginWasCalled).To(BeFalse())
				Expect(conn.CommitWasCalled).To(BeFalse())
				Expect(conn.RollbackWasCalled).To(BeFalse())
			})

			It("delegates validation errors to the ErrorWriter", func() {
				requestBody, err := json.Marshal(map[string]interface{}{})
				Expect(err).NotTo(HaveOccurred())
				request, err = http.NewRequest("PUT", "/notifications", bytes.NewBuffer(requestBody))
				Expect(err).NotTo(HaveOccurred())

				handler.ServeHTTP(writer, request, context)

				Expect(errorWriter.Error).To(BeAssignableToTypeOf(params.ValidationError{}))

				Expect(conn.BeginWasCalled).To(BeFalse())
				Expect(conn.CommitWasCalled).To(BeFalse())
				Expect(conn.RollbackWasCalled).To(BeFalse())
			})

			It("delegates registrar register errors to the ErrorWriter", func() {
				registrar.RegisterCall.Error = errors.New("BOOM!")

				handler.ServeHTTP(writer, request, context)

				Expect(errorWriter.Error).To(Equal(errors.New("BOOM!")))

				Expect(conn.BeginWasCalled).To(BeTrue())
				Expect(conn.CommitWasCalled).To(BeFalse())
				Expect(conn.RollbackWasCalled).To(BeTrue())
			})

			It("delegates registrar prune errors to the ErrorWriter", func() {
				registrar.PruneCall.Error = errors.New("BOOM!")

				handler.ServeHTTP(writer, request, context)

				Expect(errorWriter.Error).To(Equal(errors.New("BOOM!")))

				Expect(conn.BeginWasCalled).To(BeTrue())
				Expect(conn.CommitWasCalled).To(BeFalse())
				Expect(conn.RollbackWasCalled).To(BeTrue())
			})

			It("delegates transaction errors to the ErrorWriter", func() {
				conn.CommitError = "transaction commit error"
				handler.ServeHTTP(writer, request, context)

				Expect(conn.BeginWasCalled).To(BeTrue())
				Expect(conn.CommitWasCalled).To(BeTrue())
				Expect(conn.RollbackWasCalled).To(BeFalse())

				Expect(errorWriter.Error).To(Equal(errors.New("transaction commit error")))

			})
		})
	})
})
