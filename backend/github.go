package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub App integration. Each user can connect a GitHub App they created
// (Settings > GitHub). Insta Deploy then:
//   - reads private repositories with short-lived installation tokens,
//     scoped to one repository and read-only, minted when they're needed
//     (resolving a branch, or handing a clone to the agent);
//   - receives the app's push webhooks and redeploys auto-deploy
//     deployments right away. Polling (GIT_POLL_SECONDS) still runs, so
//     webhooks are optional, e.g. when the dashboard isn't reachable from
//     the internet.

const githubAPI = "https://api.github.com"

var githubClient = &http.Client{Timeout: 20 * time.Second}

type githubApp struct {
	UserID  string
	AppID   int64
	Slug    string
	Name    string
	Owner   string
	HTMLURL string

	key           *rsa.PrivateKey
	webhookSecret string
}

// parseGitHubKey reads the .pem private key GitHub generates for an app
// (PKCS#1, "BEGIN RSA PRIVATE KEY"); PKCS#8 is accepted too.
func parseGitHubKey(text string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(text)))
	if block == nil {
		return nil, errors.New("the private key must be the .pem file GitHub generated (it starts with -----BEGIN RSA PRIVATE KEY-----)")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("could not read the private key: " + err.Error())
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the private key must be an RSA key")
	}
	return rsaKey, nil
}

// jwt signs the short-lived token that authenticates as the app itself
// (used to find installations and mint installation tokens).
func (a *githubApp) jwt() (string, error) {
	enc := base64.RawURLEncoding
	now := time.Now()
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(), // allow for clock drift
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(a.AppID, 10),
	})
	signing := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

type githubError struct {
	Status  int
	Message string
}

func (e *githubError) Error() string { return fmt.Sprintf("GitHub: %s (HTTP %d)", e.Message, e.Status) }

func isGitHubStatus(err error, status int) bool {
	var ge *githubError
	return errors.As(err, &ge) && ge.Status == status
}

// githubCall calls the GitHub REST API. auth is "Bearer <jwt>" or
// "token <installation token>".
func githubCall(ctx context.Context, method, path, auth string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, githubAPI+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "insta-deploy")
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := githubClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
		return &githubError{Status: resp.StatusCode, Message: e.Message}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
	}
	return nil
}

const githubAppSelect = `SELECT user_id, app_id, slug, name, owner, html_url, private_key_encrypted, webhook_secret_encrypted FROM github_apps`

func (s *Server) scanGitHubApp(row *sql.Row) (*githubApp, error) {
	var a githubApp
	var keyEnc, secretEnc []byte
	err := row.Scan(&a.UserID, &a.AppID, &a.Slug, &a.Name, &a.Owner, &a.HTMLURL, &keyEnc, &secretEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pemText, err := s.box.Decrypt(keyEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt GitHub App key: %w", err)
	}
	if a.key, err = parseGitHubKey(pemText); err != nil {
		return nil, err
	}
	if secretEnc != nil {
		if a.webhookSecret, err = s.box.Decrypt(secretEnc); err != nil {
			return nil, fmt.Errorf("decrypt GitHub webhook secret: %w", err)
		}
	}
	return &a, nil
}

// githubAppFor returns the user's GitHub App, or nil if they haven't
// connected one.
func (s *Server) githubAppFor(ctx context.Context, uid string) (*githubApp, error) {
	return s.scanGitHubApp(s.db.QueryRowContext(ctx, githubAppSelect+` WHERE user_id = $1`, uid))
}

// ------------------------------------------------------------ tokens

// githubTokens caches installation tokens. GitHub issues them for an
// hour; they're reused until 10 minutes before they expire.
type githubTokens struct {
	sync.Mutex
	m map[string]githubToken
}

type githubToken struct {
	token   string
	expires time.Time
}

func (c *githubTokens) get(key string) string {
	c.Lock()
	defer c.Unlock()
	if t, ok := c.m[key]; ok && time.Until(t.expires) > 10*time.Minute {
		return t.token
	}
	return ""
}

func (c *githubTokens) put(key, token string, expires time.Time) {
	c.Lock()
	defer c.Unlock()
	if c.m == nil {
		c.m = map[string]githubToken{}
	}
	c.m[key] = githubToken{token, expires}
}

// forget drops an app's tokens, when it's disconnected or replaced.
func (c *githubTokens) forget(appID int64) {
	c.Lock()
	defer c.Unlock()
	prefix := strconv.FormatInt(appID, 10) + "/"
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
}

// githubRepoPath returns "owner/repo" for https://github.com/owner/repo
// (with or without .git), or "" for anything else.
func githubRepoPath(gitURL string) string {
	u, err := url.Parse(gitURL)
	if err != nil || !strings.EqualFold(u.Host, "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// installationToken mints a token for an installation, limited to the
// given repository names (all of the installation's if none) and to
// reading contents.
func (a *githubApp) installationToken(ctx context.Context, installationID int64, repos ...string) (string, time.Time, error) {
	jwt, err := a.jwt()
	if err != nil {
		return "", time.Time{}, err
	}
	body := map[string]any{"permissions": map[string]string{"contents": "read"}}
	if len(repos) > 0 {
		body["repositories"] = repos
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err = githubCall(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", installationID), "Bearer "+jwt, body, &out)
	if isGitHubStatus(err, http.StatusUnprocessableEntity) {
		err = errors.New("the GitHub App can't read repository contents. In the app's settings, set Repository permissions > Contents to Read-only, then accept the new permissions on the installation")
	}
	return out.Token, out.ExpiresAt, err
}

// gitToken returns a read-only token for repoURL from the user's GitHub
// App, or "" when there's nothing to authenticate with: the repository
// isn't on github.com, no app is connected, or the app isn't installed on
// it. Public repositories still work without a token.
func (s *Server) gitToken(ctx context.Context, uid, repoURL string) (string, error) {
	repo := githubRepoPath(repoURL)
	if repo == "" {
		return "", nil
	}
	app, err := s.githubAppFor(ctx, uid)
	if err != nil || app == nil {
		return "", err
	}
	key := fmt.Sprintf("%d/%s", app.AppID, strings.ToLower(repo))
	if t := s.github.get(key); t != "" {
		return t, nil
	}
	jwt, err := app.jwt()
	if err != nil {
		return "", err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := githubCall(ctx, "GET", "/repos/"+repo+"/installation", "Bearer "+jwt, nil, &inst); err != nil {
		if isGitHubStatus(err, http.StatusNotFound) {
			return "", nil // not installed there; try without a token
		}
		if isGitHubStatus(err, http.StatusUnauthorized) {
			return "", errors.New("GitHub rejected the GitHub App's credentials. Check the App ID and private key in Settings")
		}
		return "", err
	}
	_, name, _ := strings.Cut(repo, "/")
	token, expires, err := app.installationToken(ctx, inst.ID, name)
	if err != nil {
		return "", err
	}
	s.github.put(key, token, expires)
	return token, nil
}

// resolveGitCommit finds the commit a branch points to, using the user's
// GitHub App for private repositories.
func (s *Server) resolveGitCommit(ctx context.Context, uid, repoURL, ref string) (string, error) {
	token, err := s.gitToken(ctx, uid, repoURL)
	if err != nil {
		return "", err
	}
	return gitResolve(ctx, repoURL, ref, token)
}

// ------------------------------------------------------------ API

type githubInstallation struct {
	ID                  int64  `json:"id"`
	Account             string `json:"account"`
	RepositorySelection string `json:"repository_selection"` // all or selected
	HTMLURL             string `json:"html_url"`             // where to change its repositories
}

type githubRepo struct {
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type githubStatus struct {
	Connected         bool                 `json:"connected"`
	AppID             int64                `json:"app_id,omitempty"`
	Slug              string               `json:"slug,omitempty"`
	Name              string               `json:"name,omitempty"`
	Owner             string               `json:"owner,omitempty"`
	HTMLURL           string               `json:"html_url,omitempty"`
	InstallURL        string               `json:"install_url,omitempty"`
	WebhookSecretSet  bool                 `json:"webhook_secret_set"`
	Installations     []githubInstallation `json:"installations"`
	Repositories      []githubRepo         `json:"repositories"`
	Error             string               `json:"error,omitempty"`
	PollEverySeconds  int                  `json:"poll_every_seconds"`
	WebhookPath       string               `json:"webhook_path"`
	RepositoriesTrunc bool                 `json:"repositories_truncated,omitempty"`
}

// GET /api/github: the user's GitHub App, where it's installed and the
// repositories it can read (for the deploy form's repository picker).
func (s *Server) handleGetGitHub(w http.ResponseWriter, r *http.Request) {
	out := githubStatus{
		Installations: []githubInstallation{}, Repositories: []githubRepo{},
		PollEverySeconds: int(s.cfg.GitPollEvery / time.Second), WebhookPath: "/api/github/webhook",
	}
	app, err := s.githubAppFor(r.Context(), userID(r))
	if err != nil {
		internalError(w, err)
		return
	}
	if app == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Connected, out.AppID, out.Slug, out.Name, out.Owner, out.HTMLURL = true, app.AppID, app.Slug, app.Name, app.Owner, app.HTMLURL
	out.InstallURL = "https://github.com/apps/" + app.Slug + "/installations/new"
	out.WebhookSecretSet = app.webhookSecret != ""
	if err := s.listGitHubRepos(r.Context(), app, &out); err != nil {
		out.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listGitHubRepos(ctx context.Context, app *githubApp, out *githubStatus) error {
	jwt, err := app.jwt()
	if err != nil {
		return err
	}
	var insts []struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
		RepositorySelection string `json:"repository_selection"`
		HTMLURL             string `json:"html_url"`
	}
	if err := githubCall(ctx, "GET", "/app/installations?per_page=100", "Bearer "+jwt, nil, &insts); err != nil {
		if isGitHubStatus(err, http.StatusUnauthorized) {
			return errors.New("GitHub rejected the app's credentials. Check the App ID and generate a new private key")
		}
		return err
	}
	for _, in := range insts {
		out.Installations = append(out.Installations, githubInstallation{
			ID: in.ID, Account: in.Account.Login, RepositorySelection: in.RepositorySelection, HTMLURL: in.HTMLURL,
		})
		token, _, err := app.installationToken(ctx, in.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", in.Account.Login, err)
		}
		for page := 1; page <= 10; page++ {
			var res struct {
				Repositories []githubRepo `json:"repositories"`
			}
			if err := githubCall(ctx, "GET", fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), "token "+token, nil, &res); err != nil {
				return err
			}
			out.Repositories = append(out.Repositories, res.Repositories...)
			if len(res.Repositories) < 100 {
				break
			}
			if page == 10 {
				out.RepositoriesTrunc = true
			}
		}
	}
	sort.Slice(out.Repositories, func(i, j int) bool {
		return strings.ToLower(out.Repositories[i].FullName) < strings.ToLower(out.Repositories[j].FullName)
	})
	return nil
}

// PUT /api/github {"app_id", "private_key", "webhook_secret"} connects (or
// updates) the user's GitHub App. Empty private_key or webhook_secret keep
// the saved ones. The credentials are checked with GitHub before saving.
func (s *Server) handleSaveGitHub(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AppID         string `json:"app_id"`
		PrivateKey    string `json:"private_key"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, uid := r.Context(), userID(r)
	appID, err := strconv.ParseInt(strings.TrimSpace(req.AppID), 10, 64)
	if err != nil || appID <= 0 {
		writeError(w, http.StatusBadRequest, "enter the App ID (a number, shown at the top of the app's settings page on GitHub)")
		return
	}
	existing, err := s.githubAppFor(ctx, uid)
	if err != nil {
		internalError(w, err)
		return
	}
	app := &githubApp{UserID: uid, AppID: appID}
	keyPEM := strings.TrimSpace(req.PrivateKey)
	switch {
	case keyPEM != "":
		if app.key, err = parseGitHubKey(keyPEM); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	case existing != nil && existing.AppID == appID:
		app.key = existing.key
		keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(existing.key)}))
	default:
		writeError(w, http.StatusBadRequest, "paste the app's private key (.pem)")
		return
	}
	secret := strings.TrimSpace(req.WebhookSecret)
	if secret == "" && existing != nil {
		secret = existing.webhookSecret
	}
	if secret != "" && len(secret) < 16 {
		writeError(w, http.StatusBadRequest, "use a webhook secret of at least 16 characters (use Generate)")
		return
	}

	var taken bool
	s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM github_apps WHERE app_id = $1 AND user_id <> $2)`, appID, uid).Scan(&taken)
	if taken {
		writeError(w, http.StatusConflict, "this GitHub App is already connected to another Insta Deploy account. Create a separate app for each account")
		return
	}

	// Only save credentials that work.
	jwt, err := app.jwt()
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not sign with the private key: "+err.Error())
		return
	}
	var info struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
		Owner   struct {
			Login string `json:"login"`
		} `json:"owner"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := githubCall(ctx, "GET", "/app", "Bearer "+jwt, nil, &info); err != nil {
		msg := err.Error()
		if isGitHubStatus(err, http.StatusUnauthorized) {
			msg = "GitHub rejected these credentials. Check that the App ID and the private key belong to the same app, and that the server's clock is correct"
		}
		writeError(w, http.StatusBadGateway, msg)
		return
	}
	if info.Permissions["contents"] == "" {
		writeError(w, http.StatusBadRequest, "the app can't read code yet. On GitHub, open the app's Permissions & events and set Repository permissions > Contents to Read-only")
		return
	}

	var secretEnc any
	if secret != "" {
		secretEnc = s.box.Encrypt(secret)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO github_apps (user_id, app_id, slug, name, owner, html_url, private_key_encrypted, webhook_secret_encrypted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id) DO UPDATE SET app_id = EXCLUDED.app_id, slug = EXCLUDED.slug, name = EXCLUDED.name, owner = EXCLUDED.owner,
			html_url = EXCLUDED.html_url, private_key_encrypted = EXCLUDED.private_key_encrypted,
			webhook_secret_encrypted = EXCLUDED.webhook_secret_encrypted, updated_at = now()`,
		uid, appID, info.Slug, info.Name, info.Owner.Login, info.HTMLURL, s.box.Encrypt(keyPEM), secretEnc)
	if err != nil {
		internalError(w, err)
		return
	}
	if existing != nil {
		s.github.forget(existing.AppID)
	}
	s.event(ctx, uid, "success", "Connected GitHub App "+info.Name, eventRefs{})
	s.handleGetGitHub(w, r)
}

// DELETE /api/github disconnects the app. It stays installed on GitHub
// until it's uninstalled or deleted there.
func (s *Server) handleDeleteGitHub(w http.ResponseWriter, r *http.Request) {
	var appID int64
	err := s.db.QueryRowContext(r.Context(), `DELETE FROM github_apps WHERE user_id = $1 RETURNING app_id`, userID(r)).Scan(&appID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internalError(w, err)
		return
	}
	if appID != 0 {
		s.github.forget(appID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------ webhooks

// POST /api/github/webhook receives the app's webhooks (no session: the
// delivery is authenticated with the app's webhook secret). A push to a
// branch redeploys that user's auto-deploy deployments of the branch.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 25<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	ctx := r.Context()
	appID, _ := strconv.ParseInt(r.Header.Get("X-GitHub-Hook-Installation-Target-ID"), 10, 64)
	var app *githubApp
	if appID > 0 {
		if app, err = s.scanGitHubApp(s.db.QueryRowContext(ctx, githubAppSelect+` WHERE app_id = $1`, appID)); err != nil {
			internalError(w, err)
			return
		}
	}
	if app == nil || app.webhookSecret == "" {
		writeError(w, http.StatusNotFound, "no connected GitHub App with a webhook secret matches this delivery")
		return
	}
	if !validWebhookSignature(app.webhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeError(w, http.StatusUnauthorized, "invalid signature: check that the webhook secret on GitHub matches the one saved in Insta Deploy")
		return
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"status": "Insta Deploy is receiving webhooks"})
		return
	case "push":
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	var push struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &push); err != nil {
		writeError(w, http.StatusBadRequest, "invalid push payload")
		return
	}
	branch, isBranch := strings.CutPrefix(push.Ref, "refs/heads/")
	if !isBranch || push.Deleted || len(push.After) != 40 {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}

	list, err := s.autoDeployments(ctx, app.UserID, branch)
	if err != nil {
		internalError(w, err)
		return
	}
	queued := 0
	for _, d := range list {
		if !strings.EqualFold(githubRepoPath(d.Spec.Source.GitURL), push.Repository.FullName) {
			continue
		}
		ok, err := s.queueAutoDeploy(ctx, d, push.After, "push")
		if err != nil {
			log.Printf("github push %s: %v", d.ID, err)
		} else if ok {
			queued++
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "ok", "redeploying": queued})
}

func validWebhookSignature(secret string, body []byte, header string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
