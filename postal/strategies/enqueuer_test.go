package strategies_test

import (
	"bytes"
	"errors"
	"log"
	"time"

	"github.com/cloudfoundry-incubator/notifications/cf"
	"github.com/cloudfoundry-incubator/notifications/fakes"
	"github.com/cloudfoundry-incubator/notifications/postal/strategies"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Enqueuer", func() {
	var (
		enqueuer     strategies.Enqueuer
		logger       *log.Logger
		buffer       *bytes.Buffer
		queue        *fakes.Queue
		conn         *fakes.Connection
		space        cf.CloudControllerSpace
		org          cf.CloudControllerOrganization
		messagesRepo *fakes.MessagesRepo
		reqReceived  time.Time
	)

	BeforeEach(func() {
		buffer = bytes.NewBuffer([]byte{})
		logger = log.New(buffer, "", 0)
		queue = fakes.NewQueue()
		conn = fakes.NewConnection()
		messagesRepo = fakes.NewMessagesRepo()
		enqueuer = strategies.NewEnqueuer(queue, fakes.NewIncrementingGUIDGenerator().Generate, messagesRepo)
		space = cf.CloudControllerSpace{Name: "the-space"}
		org = cf.CloudControllerOrganization{Name: "the-org"}
		reqReceived, _ = time.Parse(time.RFC3339Nano, "2015-06-08T14:40:12.207187819-07:00")
	})

	Describe("Enqueue", func() {
		It("returns the correct types of responses for users", func() {
			users := []strategies.User{{GUID: "user-1"}, {Email: "user-2@example.com"}, {GUID: "user-3"}, {GUID: "user-4"}}
			responses := enqueuer.Enqueue(conn, users, strategies.Options{KindID: "the-kind"}, space, org, "the-client", "my.scope", "some-request-id", reqReceived)

			Expect(responses).To(HaveLen(4))
			Expect(responses).To(ConsistOf([]strategies.Response{
				{
					Status:         "queued",
					Recipient:      "user-1",
					NotificationID: "deadbeef-aabb-ccdd-eeff-001122334455",
					VCAPRequestID:  "some-request-id",
				},
				{
					Status:         "queued",
					Recipient:      "user-2@example.com",
					NotificationID: "deadbeef-aabb-ccdd-eeff-001122334456",
					VCAPRequestID:  "some-request-id",
				},
				{
					Status:         "queued",
					Recipient:      "user-3",
					NotificationID: "deadbeef-aabb-ccdd-eeff-001122334457",
					VCAPRequestID:  "some-request-id",
				},
				{
					Status:         "queued",
					Recipient:      "user-4",
					NotificationID: "deadbeef-aabb-ccdd-eeff-001122334458",
					VCAPRequestID:  "some-request-id",
				},
			}))
		})

		It("enqueues jobs with the deliveries", func() {
			users := []strategies.User{{GUID: "user-1"}, {GUID: "user-2"}, {GUID: "user-3"}, {GUID: "user-4"}}
			enqueuer.Enqueue(conn, users, strategies.Options{}, space, org, "the-client", "my.scope", "some-request-id", reqReceived)

			var deliveries []strategies.Delivery
			for _ = range users {
				job := <-queue.Reserve("me")
				var delivery strategies.Delivery
				err := job.Unmarshal(&delivery)
				if err != nil {
					panic(err)
				}
				deliveries = append(deliveries, delivery)
			}

			Expect(deliveries).To(HaveLen(4))
			Expect(deliveries).To(ConsistOf([]strategies.Delivery{
				{
					Options:         strategies.Options{},
					UserGUID:        "user-1",
					Space:           space,
					Organization:    org,
					ClientID:        "the-client",
					MessageID:       "deadbeef-aabb-ccdd-eeff-001122334455",
					Scope:           "my.scope",
					VCAPRequestID:   "some-request-id",
					RequestReceived: reqReceived,
				},
				{
					Options:         strategies.Options{},
					UserGUID:        "user-2",
					Space:           space,
					Organization:    org,
					ClientID:        "the-client",
					MessageID:       "deadbeef-aabb-ccdd-eeff-001122334456",
					Scope:           "my.scope",
					VCAPRequestID:   "some-request-id",
					RequestReceived: reqReceived,
				},
				{
					Options:         strategies.Options{},
					UserGUID:        "user-3",
					Space:           space,
					Organization:    org,
					ClientID:        "the-client",
					MessageID:       "deadbeef-aabb-ccdd-eeff-001122334457",
					Scope:           "my.scope",
					VCAPRequestID:   "some-request-id",
					RequestReceived: reqReceived,
				},
				{
					Options:         strategies.Options{},
					UserGUID:        "user-4",
					Space:           space,
					Organization:    org,
					ClientID:        "the-client",
					MessageID:       "deadbeef-aabb-ccdd-eeff-001122334458",
					Scope:           "my.scope",
					VCAPRequestID:   "some-request-id",
					RequestReceived: reqReceived,
				},
			}))
		})

		It("Upserts a StatusQueued for each of the jobs", func() {
			users := []strategies.User{{GUID: "user-1"}, {GUID: "user-2"}, {GUID: "user-3"}, {GUID: "user-4"}}
			enqueuer.Enqueue(conn, users, strategies.Options{}, space, org, "the-client", "my.scope", "some-request-id", reqReceived)

			var statuses []string
			for _ = range users {
				job := <-queue.Reserve("me")
				var delivery strategies.Delivery
				err := job.Unmarshal(&delivery)
				if err != nil {
					panic(err)
				}

				message, err := messagesRepo.FindByID(conn, delivery.MessageID)
				if err != nil {
					panic(err)
				}

				statuses = append(statuses, message.Status)
			}

			Expect(statuses).To(HaveLen(4))
			Expect(statuses).To(ConsistOf([]string{strategies.StatusQueued, strategies.StatusQueued, strategies.StatusQueued, strategies.StatusQueued}))
		})

		Context("using a transaction", func() {
			It("commits the transaction when everything goes well", func() {
				users := []strategies.User{{GUID: "user-1"}, {GUID: "user-2"}, {GUID: "user-3"}, {GUID: "user-4"}}
				responses := enqueuer.Enqueue(conn, users, strategies.Options{}, space, org, "the-client", "my.scope", "some-request-id", reqReceived)

				Expect(conn.BeginWasCalled).To(BeTrue())
				Expect(conn.CommitWasCalled).To(BeTrue())
				Expect(conn.RollbackWasCalled).To(BeFalse())
				Expect(responses).ToNot(BeEmpty())
			})

			It("rolls back the transaction when there is an error in message repo upserting", func() {
				messagesRepo.UpsertError = errors.New("BOOM!")
				users := []strategies.User{{GUID: "user-1"}}
				enqueuer.Enqueue(conn, users, strategies.Options{}, space, org, "the-client", "my.scope", "some-request-id", reqReceived)

				Expect(conn.BeginWasCalled).To(BeTrue())
				Expect(conn.CommitWasCalled).To(BeFalse())
				Expect(conn.RollbackWasCalled).To(BeTrue())
			})

			It("returns an empty slice of Response if transaction fails", func() {
				conn.CommitError = "the commit blew up"
				users := []strategies.User{{GUID: "user-1"}, {GUID: "user-2"}, {GUID: "user-3"}, {GUID: "user-4"}}
				responses := enqueuer.Enqueue(conn, users, strategies.Options{}, space, org, "the-client", "my.scope", "some-request-id", reqReceived)

				Expect(conn.BeginWasCalled).To(BeTrue())
				Expect(conn.CommitWasCalled).To(BeTrue())
				Expect(conn.RollbackWasCalled).To(BeFalse())
				Expect(responses).To(Equal([]strategies.Response{}))
			})
		})
	})
})
