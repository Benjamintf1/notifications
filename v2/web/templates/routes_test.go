package templates_test

import (
	"database/sql"
	"net/http"

	"github.com/cloudfoundry-incubator/notifications/testing/mocks"
	"github.com/cloudfoundry-incubator/notifications/v2/collections"
	"github.com/cloudfoundry-incubator/notifications/v2/web/middleware"
	"github.com/cloudfoundry-incubator/notifications/v2/web/templates"
	"github.com/cloudfoundry-incubator/notifications/web"
	"github.com/pivotal-golang/lager"
	"github.com/ryanmoran/stack"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Routes", func() {
	var (
		logging     middleware.RequestLogging
		dbAllocator middleware.DatabaseAllocator
		writeAuth   middleware.Authenticator
		adminAuth   middleware.Authenticator
		muxer       web.Muxer
	)

	BeforeEach(func() {
		logging = middleware.NewRequestLogging(lager.NewLogger("log-prefix"), mocks.NewClock())
		writeAuth = middleware.NewAuthenticator(&mocks.TokenValidator{}, "notifications.write")
		adminAuth = middleware.NewAuthenticator(&mocks.TokenValidator{}, "notifications.admin")
		dbAllocator = middleware.NewDatabaseAllocator(&sql.DB{}, false)
		muxer = web.NewMuxer()
		templates.Routes{
			RequestLogging:      logging,
			WriteAuthenticator:  writeAuth,
			AdminAuthenticator:  adminAuth,
			DatabaseAllocator:   dbAllocator,
			TemplatesCollection: collections.TemplatesCollection{},
		}.Register(muxer)
	})

	It("routes POST /templates", func() {
		request, err := http.NewRequest("POST", "/templates", nil)
		Expect(err).NotTo(HaveOccurred())

		s := muxer.Match(request).(stack.Stack)
		Expect(s.Handler).To(BeAssignableToTypeOf(templates.CreateHandler{}))
		Expect(s.Middleware).To(HaveLen(3))

		requestLogging := s.Middleware[0].(middleware.RequestLogging)
		Expect(requestLogging).To(Equal(logging))

		authenticator := s.Middleware[1].(middleware.Authenticator)
		Expect(authenticator).To(Equal(writeAuth))

		databaseAllocator := s.Middleware[2].(middleware.DatabaseAllocator)
		Expect(databaseAllocator).To(Equal(dbAllocator))
	})

	It("routes GET /templates/ID", func() {
		request, err := http.NewRequest("GET", "/templates/some-template-id", nil)
		Expect(err).NotTo(HaveOccurred())

		s := muxer.Match(request).(stack.Stack)
		Expect(s.Handler).To(BeAssignableToTypeOf(templates.GetHandler{}))
		Expect(s.Middleware).To(HaveLen(3))

		requestLogging := s.Middleware[0].(middleware.RequestLogging)
		Expect(requestLogging).To(Equal(logging))

		authenticator := s.Middleware[1].(middleware.Authenticator)
		Expect(authenticator).To(Equal(writeAuth))

		databaseAllocator := s.Middleware[2].(middleware.DatabaseAllocator)
		Expect(databaseAllocator).To(Equal(dbAllocator))
	})

	It("routes DELETE /templates/ID", func() {
		request, err := http.NewRequest("DELETE", "/templates/some-template-id", nil)
		Expect(err).NotTo(HaveOccurred())

		s := muxer.Match(request).(stack.Stack)
		Expect(s.Handler).To(BeAssignableToTypeOf(templates.DeleteHandler{}))
		Expect(s.Middleware).To(HaveLen(3))

		requestLogging := s.Middleware[0].(middleware.RequestLogging)
		Expect(requestLogging).To(Equal(logging))

		authenticator := s.Middleware[1].(middleware.Authenticator)
		Expect(authenticator).To(Equal(writeAuth))

		databaseAllocator := s.Middleware[2].(middleware.DatabaseAllocator)
		Expect(databaseAllocator).To(Equal(dbAllocator))
	})

	It("routes PUT /templates/ID", func() {
		request, err := http.NewRequest("PUT", "/templates/some-template-id", nil)
		Expect(err).NotTo(HaveOccurred())

		s := muxer.Match(request).(stack.Stack)
		Expect(s.Handler).To(BeAssignableToTypeOf(templates.UpdateHandler{}))
		Expect(s.Middleware).To(HaveLen(3))

		requestLogging := s.Middleware[0].(middleware.RequestLogging)
		Expect(requestLogging).To(Equal(logging))

		authenticator := s.Middleware[1].(middleware.Authenticator)
		Expect(authenticator).To(Equal(writeAuth))

		databaseAllocator := s.Middleware[2].(middleware.DatabaseAllocator)
		Expect(databaseAllocator).To(Equal(dbAllocator))
	})

	It("routes PUT /templates/default", func() {
		request, err := http.NewRequest("PUT", "/templates/default", nil)
		Expect(err).NotTo(HaveOccurred())

		s := muxer.Match(request).(stack.Stack)
		Expect(s.Handler).To(BeAssignableToTypeOf(templates.UpdateDefaultHandler{}))
		Expect(s.Middleware).To(HaveLen(3))

		requestLogging := s.Middleware[0].(middleware.RequestLogging)
		Expect(requestLogging).To(Equal(logging))

		authenticator := s.Middleware[1].(middleware.Authenticator)
		Expect(authenticator).To(Equal(adminAuth))

		databaseAllocator := s.Middleware[2].(middleware.DatabaseAllocator)
		Expect(databaseAllocator).To(Equal(dbAllocator))
	})

	It("routes GET /templates", func() {
		request, err := http.NewRequest("GET", "/templates", nil)
		Expect(err).NotTo(HaveOccurred())

		s := muxer.Match(request).(stack.Stack)
		Expect(s.Handler).To(BeAssignableToTypeOf(templates.ListHandler{}))
		Expect(s.Middleware).To(HaveLen(3))

		requestLogging := s.Middleware[0].(middleware.RequestLogging)
		Expect(requestLogging).To(Equal(logging))

		authenticator := s.Middleware[1].(middleware.Authenticator)
		Expect(authenticator).To(Equal(writeAuth))

		databaseAllocator := s.Middleware[2].(middleware.DatabaseAllocator)
		Expect(databaseAllocator).To(Equal(dbAllocator))
	})
})
