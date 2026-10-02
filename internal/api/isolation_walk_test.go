package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// walkRoutes lists every method and pattern the router serves.
func walkRoutes(r chi.Routes, fn func(method, route string)) {
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		fn(method, route)
		return nil
	})
}
