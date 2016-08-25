package turnservicecli

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A TURNCredentialsHandler is a function handler which can be registered to
// get called when the cached TURN credentials change.
type TURNCredentialsHandler func(*CachedCredentialsData, error)

// A TURNService provides the TURN service remote API.
type TURNService struct {
	sync.RWMutex

	uri                  string
	tlsConfig            *tls.Config
	expirationPercentile uint

	session     string
	accessToken string
	clientID    string

	credentials *CachedCredentialsData
	err         error
	autorefresh bool

	handlers []TURNCredentialsHandler
	refresh  chan bool
	quit     chan bool
}

// NewTURNService creates a TURNService.
func NewTURNService(uri string, expirationPercentile uint, tlsConfig *tls.Config) *TURNService {
	if expirationPercentile == 0 {
		expirationPercentile = 80
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			ClientSessionCache: tls.NewLRUClientSessionCache(0),
			InsecureSkipVerify: false,
		}
	}

	service := &TURNService{
		uri:                  uri,
		tlsConfig:            tlsConfig,
		expirationPercentile: expirationPercentile,
		quit:                 make(chan bool),
		refresh:              make(chan bool, 1),
	}
	go func() {
		// Check for refresh every minute.
		ticker := time.NewTicker(1 * time.Minute)
		autorefresh := false
		for {
			select {
			case <-service.quit:
				ticker.Stop()
				return
			case <-service.refresh:
			case <-ticker.C:
			}

			service.RLock()
			autorefresh = service.autorefresh
			service.RUnlock()
			if autorefresh {
				service.Credentials(true)
			}
		}
	}()

	return service
}

// Open sets the data to use for requests to the TURNService.
func (service *TURNService) Open(accessToken, clientID, session string) {
	service.Lock()
	defer service.Unlock()
	service.accessToken = accessToken
	service.clientID = clientID
	service.session = session
}

// Close expires all data and resets the data to use with the TURNService.
func (service *TURNService) Close() {
	service.Lock()
	defer service.Unlock()
	close(service.quit)
	if service.credentials != nil {
		service.credentials.Close()
	}
	service.accessToken = ""
	service.clientID = ""
	service.session = ""
}

// Autorefresh enables or disables automatic refresh of TURNService credentials.
func (service *TURNService) Autorefresh(autorefresh bool) {
	service.Lock()
	defer service.Unlock()
	if autorefresh == service.autorefresh {
		return
	}
	service.autorefresh = autorefresh
	if autorefresh {
		// Trigger instant refresh, do not care if already pending.
		select {
		case service.refresh <- true:
		default:
		}
	}
}

// BindOnCredentials triggeres whenever new TURN credentials become available.
func (service *TURNService) BindOnCredentials(h TURNCredentialsHandler) {
	service.Lock()
	defer service.Unlock()
	service.handlers = append(service.handlers, h)
}

// Credentials implements the credentials API call to the TURNService returning
// cached credential data when those are not yet expired.
func (service *TURNService) Credentials(fetch bool) *CachedCredentialsData {
	service.RLock()
	credentials := service.credentials
	accessToken := service.accessToken
	clientID := service.clientID
	session := service.session
	service.RUnlock()

	var err error
	var response *CredentialsResponse

	if credentials == nil {
		// No credentials.
		if !fetch {
			return nil
		}

		service.Lock()
		defer service.Unlock()
		if service.credentials == nil {
			response, err = service.fetchCredentials(accessToken, clientID, session)
			if err != nil {
				service.err = err
			}
		} else {
			credentials = service.credentials
		}
	} else {
		if credentials.Expired() {
			// Expired credentials.
			if fetch {
				service.Lock()
				defer service.Unlock()
				if service.credentials == nil || service.credentials.Expired() {
					response, err = service.fetchCredentials(accessToken, clientID, session)
					service.err = err
				} else {
					credentials = service.credentials
				}
			} else {
				credentials = nil
			}
		}
	}

	if response != nil && err == nil {
		credentials = NewCachedCredentialsData(response.Turn, service.expirationPercentile)
		// Already locked from above if response is not nil.
		service.credentials = credentials
		service.session = response.Session
	}

	// Trigger registered handlers.
	for _, h := range service.handlers {
		go h(credentials, err)
	}

	return credentials
}

// LastError returns the last occured Error if any.
func (service *TURNService) LastError() error {
	service.RLock()
	defer service.RUnlock()
	return service.err
}

// FetchCredentials fetches new TURN credentials via the remote service.
func (service *TURNService) FetchCredentials() (*CredentialsResponse, error) {
	service.RLock()
	accessToken := service.accessToken
	clientID := service.clientID
	session := service.session
	service.RUnlock()

	return service.fetchCredentials(accessToken, clientID, session)
}

func (service *TURNService) fetchCredentials(accessToken, clientID, session string) (*CredentialsResponse, error) {
	if accessToken == "" && clientID == "" {
		return nil, fmt.Errorf("One of accessToken/clientId must be set")
	}

	var body *bytes.Buffer
	nonce := "make-me-random" //XXX(longsleep): Create random nonce.

	data := url.Values{}
	data.Set("nonce", nonce)
	data.Set("client_id", clientID)
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", accessToken, session)))
	body = bytes.NewBufferString(data.Encode())

	request, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/turn/credentials", service.uri), body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     service.tlsConfig,
		TLSHandshakeTimeout: time.Second * 30,
	}

	client := &http.Client{
		Transport: transport,
	}

	result, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()

	switch result.StatusCode {
	case 200:
	case 403:
		content, _ := ioutil.ReadAll(result.Body)
		return nil, fmt.Errorf("forbidden: %s", content)
	default:
		return nil, fmt.Errorf("credentials return wrong status: %d", result.StatusCode)
	}

	var response CredentialsResponse
	err = json.NewDecoder(result.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	if !response.Success {
		return &response, fmt.Errorf("credentials response unsuccessfull")
	}

	if response.Nonce != nonce {
		return &response, fmt.Errorf("invalid nonce")
	}

	return &response, nil
}
