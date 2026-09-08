package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dzforum/internal/mfa"
)

func mfaTestServer(t *testing.T) {
	t.Helper()
	requireDB(t)
	old := smokeSrv
	cfg := old.cfg
	cfg.MFAKey = strings.Repeat("ab", 32)
	var err error
	smokeSrv, err = New(cfg, old.st, old.hub, old.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { smokeSrv = old })
}

func mfaAPIData(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	envelope := checkJSON(t, rec, status)
	var data map[string]any
	if raw := envelope["data"]; raw != nil {
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

func TestMFAJSONAndFormLogin(t *testing.T) {
	mfaTestServer(t)
	ctx := context.Background()
	u, cookie, csrf := memberTestUser(t)
	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		return memberJSON(t, "POST", path, body, csrf, cookie)
	}
	checkJSON(t, memberJSON(t, "POST", "/api/v1/me/2fa/setup", map[string]any{"password": "password123"}, "", cookie), 403)
	checkJSON(t, post("/api/v1/me/2fa/setup", map[string]any{}), 401)
	setup := mfaAPIData(t, post("/api/v1/me/2fa/setup", map[string]any{"password": "password123"}), 200)
	if setup["recoveryCodes"] != nil || !strings.HasPrefix(setup["otpauthUrl"].(string), "otpauth://totp/") {
		t.Fatal(setup)
	}
	secret := setup["secret"].(string)
	enabled := mfaAPIData(t, post("/api/v1/me/2fa/enable", map[string]any{"password": "password123", "setupId": setup["setupId"], "code": mfa.Code(secret, time.Now().Unix()/30)}), 200)
	codes := enabled["recoveryCodes"].([]any)
	response := smokeGet(t, "/api/v1/me/2fa", cookie)
	checkJSON(t, response, 200)
	if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), codes[0].(string)) {
		t.Fatal("GET exposed secret")
	}
	checkJSON(t, post("/api/v1/me/2fa/disable", map[string]any{"password": "password123"}), 401)
	anon := &http.Cookie{Name: cookieCSRF, Value: strings.Repeat("c", 32)}
	login := memberJSON(t, "POST", "/api/v1/auth/login", map[string]any{"username": u.Username, "password": "password123"}, anon.Value, anon)
	challenge := mfaAPIData(t, login, 401)["challenge"].(string)
	for _, c := range login.Result().Cookies() {
		if c.Name == cookieSession {
			t.Fatal("session before second factor")
		}
	}
	verify := func(body map[string]any) *httptest.ResponseRecorder {
		return memberJSON(t, "POST", "/api/v1/auth/2fa", body, anon.Value, anon)
	}
	checkJSON(t, verify(map[string]any{"challenge": challenge, "code": "bad"}), 401)
	// Enrollment consumed the current step; the next step is within clock skew.
	valid := verify(map[string]any{"challenge": challenge, "code": mfa.Code(secret, time.Now().Unix()/30+1)})
	checkJSON(t, valid, 200)
	var logged *http.Cookie
	for _, c := range valid.Result().Cookies() {
		if c.Name == cookieSession {
			logged = c
		}
	}
	if logged == nil {
		t.Fatal("missing session cookie")
	}
	checkJSON(t, smokeGet(t, "/api/v1/me", logged), 200)
	checkJSON(t, verify(map[string]any{"challenge": challenge, "recovery": codes[0]}), 401)
	next := mfaAPIData(t, memberJSON(t, "POST", "/api/v1/auth/login", map[string]any{"username": u.Username, "password": "password123"}, anon.Value, anon), 401)["challenge"].(string)
	values := url.Values{"challenge": {next}, "recovery": {codes[0].(string)}}
	req := httptest.NewRequest("POST", "/api/v1/auth/2fa", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", anon.Value)
	req.AddCookie(anon)
	form := httptest.NewRecorder()
	smokeSrv.Handler().ServeHTTP(form, req)
	checkJSON(t, form, 200)
	state, err := smokeSrv.st.MFA(ctx, u.ID)
	if err != nil || len(state.RecoveryHashes) != 9 {
		t.Fatal(err)
	}
	checkJSON(t, post("/api/v1/me/2fa/recovery-codes", map[string]any{"password": "password123", "recovery": codes[1]}), 200)
	checkJSON(t, smokeGet(t, "/api/v1/me", logged), 401)
}

func TestMFAUnavailableFailsClosed(t *testing.T) {
	mfaTestServer(t)
	u, cookie, csrf := memberTestUser(t)
	setup := mfaAPIData(t, memberJSON(t, "POST", "/api/v1/me/2fa/setup", map[string]any{"password": "password123"}, csrf, cookie), 200)
	mfaAPIData(t, memberJSON(t, "POST", "/api/v1/me/2fa/enable", map[string]any{"password": "password123", "setupId": setup["setupId"], "code": mfa.Code(setup["secret"].(string), time.Now().Unix()/30)}, csrf, cookie), 200)
	anon := &http.Cookie{Name: cookieCSRF, Value: strings.Repeat("d", 32)}
	challenge := mfaAPIData(t, memberJSON(t, "POST", "/api/v1/auth/login", map[string]any{"username": u.Username, "password": "password123"}, anon.Value, anon), 401)["challenge"].(string)
	good := smokeSrv.mfaCipher
	for _, key := range []string{"", strings.Repeat("cd", 32)} {
		smokeSrv.mfaCipher = nil
		if key != "" {
			smokeSrv.mfaCipher, _ = mfa.NewCipher(key)
		}
		rec := memberJSON(t, "POST", "/api/v1/auth/2fa", map[string]any{"challenge": challenge, "code": mfa.Code(setup["secret"].(string), time.Now().Unix()/30+1)}, anon.Value, anon)
		checkJSON(t, rec, 503)
		for _, c := range rec.Result().Cookies() {
			if c.Name == cookieSession {
				t.Fatal("unavailable key issued session")
			}
		}
		if key == "" {
			checkJSON(t, memberJSON(t, "POST", "/api/v1/auth/login", map[string]any{"username": u.Username, "password": "password123"}, anon.Value, anon), 503)
		}
		checkJSON(t, memberJSON(t, "POST", "/api/v1/me/2fa/disable", map[string]any{"password": "password123", "code": "123456"}, csrf, cookie), 503)
	}
	smokeSrv.mfaCipher = good
	checkJSON(t, memberJSON(t, "POST", "/api/v1/auth/2fa", map[string]any{"challenge": challenge, "code": mfa.Code(setup["secret"].(string), time.Now().Unix()/30+1)}, anon.Value, anon), 200)
	// A failed MFA lookup must fail password login, including for an ordinary user.
	ordinary, err := smokeSrv.st.CreateUser(context.Background(), "ordinary_"+t.Name(), "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := smokePool.Exec(context.Background(), `ALTER TABLE user_mfa RENAME TO test_mfa_unavailable`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = smokePool.Exec(context.Background(), `ALTER TABLE test_mfa_unavailable RENAME TO user_mfa`)
	})
	checkJSON(t, memberJSON(t, "POST", "/api/v1/auth/login", map[string]any{"username": ordinary.Username, "password": "password123"}, anon.Value, anon), 500)
}
