package modelclient

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func secureProviderHTTPClient(baseURL string, client *http.Client) (*http.Client, error) {
	expected, err := url.Parse(baseURL)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return nil, errors.New("modelclient: invalid provider base URL")
	}
	var secured http.Client
	if client != nil {
		secured = *client
	}
	previousRedirect := secured.CheckRedirect
	secured.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !sameProviderOrigin(expected, request.URL) {
			return http.ErrUseLastResponse
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("modelclient: stopped after 10 redirects")
		}
		return nil
	}
	return &secured, nil
}

func sameProviderOrigin(expected, actual *url.URL) bool {
	return actual != nil && actual.User == nil &&
		strings.EqualFold(expected.Scheme, actual.Scheme) &&
		strings.EqualFold(expected.Hostname(), actual.Hostname()) &&
		effectiveURLPort(expected) == effectiveURLPort(actual)
}

func effectiveURLPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
