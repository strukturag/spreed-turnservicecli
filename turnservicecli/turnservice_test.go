package turnservicecli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"
)

var ServiceURI string
var ClientID string
var AccessToken string
var HMacSecret string

func TestMain(m *testing.M) {
	ServiceURI = os.Getenv("SERVICE_URI")
	AccessToken = os.Getenv("ACCESS_TOKEN")
	ClientID = os.Getenv("CLIENT_ID")
	HMacSecret = os.Getenv("HMAC_SECRET")

	res := m.Run()
	os.Exit(res)
}

func TestTURNServiceCredentials(t *testing.T) {
	if ServiceURI == "" {
		t.SkipNow()
	}

	if HMacSecret != "" {
		mac := hmac.New(sha256.New, []byte(HMacSecret))
		mac.Write([]byte(ClientID))
		AccessToken = fmt.Sprintf("h%s", hex.EncodeToString(mac.Sum(nil)))
	}

	turnService := NewTURNService(ServiceURI, 0, nil)
	turnService.Open(AccessToken, ClientID, "")

	turn := turnService.Credentials(false)
	if turn != nil {
		t.Errorf("initial non-refresh data must be nil")
	}

	turn = turnService.Credentials(true)
	if turn == nil {
		t.Errorf("turn data must not be nil: %s", turnService.LastError().Error())
	}
	if turnService.session == "" {
		t.Fatalf("session must not be empty: %s", turnService.session)
	}

	if turn.Turn.Password == "" {
		t.Errorf("turn data passwurd must not be empty")
	}

	turn2 := turnService.Credentials(false)
	if turn != turn2 {
		t.Error("turn2 must be turn2")
	}

	response, err := turnService.FetchCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if turnService.session != response.Session {
		t.Errorf("session does not match previous value: %s - %s", turnService.session, response.Session)
	}

	turnService.Autorefresh(true)
	time.Sleep(2 * time.Second)

	if turn.Expired() {
		t.Errorf("turn must not be expired")
	}

	if turn2.Expired() {
		t.Errorf("turn2 must not be expired")
	}

	turn.Close()
	if !turn.Expired() {
		t.Errorf("turn must be expired after Expire()")
	}

}
