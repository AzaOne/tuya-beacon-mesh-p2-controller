package provision

import (
	"net/http"
	"net/http/httptest"
)

const testOrigin = "https://tuya.test"

// Exercise the client's request and response handling without binding a TCP port.
type handlerTransport struct{ handler http.Handler }

func (tr handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		defer r.Body.Close()
	}
	response := httptest.NewRecorder()
	tr.handler.ServeHTTP(response, r)
	return response.Result(), nil
}

func clientFor(handler http.HandlerFunc) *http.Client {
	return &http.Client{Transport: handlerTransport{handler}}
}
