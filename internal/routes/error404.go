package routes

import "net/http"

func Handler404(request Request) (int, error, any) {
	if request.Method == http.MethodOptions {
		return http.StatusOK, nil, nil
	}

	return http.StatusNotFound, nil, map[string]any{
		"error": http.StatusText(http.StatusNotFound),
	}
}
