package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Route struct {
	Method  string
	Path    string
	Handler func(request Request) (int, error, any)
}

type HTTPRoute struct {
	Method  string
	Path    string
	Handler http.Handler
}

type Request struct {
	Body      map[string]any
	Method    string
	Context   context.Context
	IP        string
	UserAgent string
}

func Run(ctx context.Context, addr string, routes []Route, httpRoutes []HTTPRoute) error {
	routes = append(routes, Route{Path: "/", Handler: Handler404})

	mux := http.NewServeMux()

	for _, route := range httpRoutes {
		mux.Handle(httpRoutePattern(route), route.Handler)
	}

	for _, route := range routes {
		mux.HandleFunc(routePattern(route), func(writer http.ResponseWriter, request *http.Request) {
			start := time.Now()

			var (
				body       map[string]any
				status     int
				errHandler error
				content    any
			)

			bodyBytes, errReadBody := io.ReadAll(request.Body)
			if errReadBody != nil {
				status, errHandler, content = http.StatusBadRequest, errReadBody, nil
			}

			if len(bodyBytes) == 0 {
				bodyBytes = []byte("{}")
			}

			if errHandler == nil {
				errUnmarshal := json.Unmarshal(bodyBytes, &body)
				if errUnmarshal != nil {
					status, errHandler, content = http.StatusBadRequest, errUnmarshal, nil
				}
			}

			req := Request{
				Body:      body,
				Method:    request.Method,
				Context:   request.Context(),
				IP:        GetRealIP(request),
				UserAgent: request.UserAgent(),
			}

			if errHandler == nil {
				status, errHandler, content = route.Handler(req)
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

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
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

func GetRealIP(request *http.Request) string {
	ip := request.Header.Get("X-Forwarded-For")
	if ip != "" {
		list := strings.Split(ip, ",")
		if len(list) > 0 {
			return strings.TrimSpace(list[0])
		}
	}

	address, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return address
	}

	return strings.TrimSpace(request.RemoteAddr)
}

func routePattern(route Route) string {
	if route.Method == "" {
		return route.Path
	}

	return route.Method + " " + route.Path
}

func httpRoutePattern(route HTTPRoute) string {
	if route.Method == "" {
		return route.Path
	}

	return route.Method + " " + route.Path
}
