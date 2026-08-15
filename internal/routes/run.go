package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Route struct {
	Method       string
	Path         string
	AuthRequired bool
	Handler      func(request Request) (int, error, any)
}

type HTTPRoute struct {
	Method  string
	Path    string
	Handler http.Handler
}

type User struct {
	Login string
	Token string
}

type UserResolver func(token string) *User

type Request struct {
	Body      map[string]any
	Method    string
	Context   context.Context
	IP        string
	UserAgent string
	User      *User
}

func Run(ctx context.Context, addr string, routes []Route, httpRoutes []HTTPRoute, resolveUser UserResolver) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           NewHandler(routes, httpRoutes, resolveUser),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Err(err).Msg("shutdown http server")
		}
	}()

	log.Info().Str("addr", addr).Msg("starting http server")

	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func NewHandler(routes []Route, httpRoutes []HTTPRoute, resolveUser UserResolver) http.Handler {
	routes = append(slices.Clone(routes), Route{Path: "/", Handler: Handler404})

	mux := http.NewServeMux()

	for _, route := range httpRoutes {
		mux.Handle(routePattern(route.Method, route.Path), route.Handler)
	}

	for _, route := range routes {
		mux.HandleFunc(routePattern(route.Method, route.Path), func(writer http.ResponseWriter, request *http.Request) {
			start := time.Now()

			req := Request{
				Method:    request.Method,
				Context:   request.Context(),
				IP:        GetRealIP(request),
				UserAgent: request.UserAgent(),
			}

			authorization := strings.TrimSpace(request.Header.Get("Authorization"))
			if scheme, token, found := strings.Cut(authorization, " "); found && strings.EqualFold(scheme, "Bearer") {
				token = strings.TrimSpace(token)
				if token != "" && resolveUser != nil {
					req.User = resolveUser(token)
				}
			}

			var (
				status     int
				errHandler error
				content    any
			)

			if route.AuthRequired && req.User == nil {
				status = http.StatusUnauthorized
				content = map[string]any{"error": http.StatusText(http.StatusUnauthorized)}
			} else {
				bodyBytes, errReadBody := io.ReadAll(request.Body)
				if errReadBody != nil {
					status, errHandler = http.StatusBadRequest, errReadBody
				}

				if len(bodyBytes) == 0 {
					bodyBytes = []byte("{}")
				}

				if errHandler == nil {
					if errUnmarshal := json.Unmarshal(bodyBytes, &req.Body); errUnmarshal != nil {
						status, errHandler = http.StatusBadRequest, errUnmarshal
					}
				}

				if errHandler == nil {
					status, errHandler, content = route.Handler(req)
				}
			}

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Headers", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,POST,PUT,DELETE,OPTIONS")

			if content == nil {
				content = struct{}{}
			}

			payload, errMarshal := json.Marshal(content)
			if errMarshal != nil {
				status = http.StatusInternalServerError
				errHandler = errMarshal
				payload = []byte(`{"error":"Internal Server Error"}`)
			}

			if status == 0 {
				status = http.StatusOK
			}
			if status != http.StatusOK {
				writer.WriteHeader(status)
			}

			length, errWrite := writer.Write(payload)
			if errWrite != nil {
				log.Err(errWrite).Msg("write content")
			}

			log.Info().
				Str("ip", req.IP).
				Str("method", request.Method).
				Str("host", request.Host).
				Str("path", request.URL.String()).
				Str("version", request.Proto).
				Int("status", status).
				Int("length", length).
				Str("useragent", request.UserAgent()).
				Dur("duration", time.Since(start)).
				Err(errHandler).
				Msg("http")
		})
	}

	return mux
}

func GetRealIP(request *http.Request) string {
	ip := request.Header.Get("X-Forwarded-For")
	if ip != "" {
		first, _, _ := strings.Cut(ip, ",")
		return strings.TrimSpace(first)
	}

	address, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return address
	}

	return strings.TrimSpace(request.RemoteAddr)
}

func routePattern(method string, path string) string {
	if method == "" {
		return path
	}

	return method + " " + path
}
