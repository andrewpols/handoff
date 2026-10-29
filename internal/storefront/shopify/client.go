package shopify

import (
	"net/http"
	"os"
	"time"
)

type authTransport struct {
	token string
	base  http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func newShopifyClient() *http.Client {
	transport := authTransport{
		token: os.Getenv("SHOPIFY_API_TOKEN"),
		base:  http.DefaultTransport,
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &transport,
	}
}
