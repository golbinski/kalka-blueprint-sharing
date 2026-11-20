package main

// The Jira REST client: Cloud (REST v3, an email and an API token) and
// Data Center (REST v2, a personal access token). Everything from a
// blueprint goes into a URL path segment or a JSON body built here,
// never into a shell; issue keys are validated before any request.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Flavors of Jira.
const (
	Cloud      = "cloud"
	DataCenter = "datacenter"
)

// keyRe is a Jira issue key: a project key and a number.
var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[1-9][0-9]{0,15}$`)

// idRe is a Jira attachment or comment id.
var idRe = regexp.MustCompile(`^[0-9]{1,20}$`)

func validKey(k string) bool { return len(k) <= 64 && keyRe.MatchString(k) }

type client struct {
	site   string // https://example.atlassian.net, no trailing slash
	flavor string
	email  string
	token  string
	http   *http.Client
	// sleep waits for a backoff; tests replace it.
	sleep func(context.Context, time.Duration) error
	// maxRetries on 429 and 503.
	maxRetries int
}

func newClient(c *siteConfig, token string) *client {
	return &client{site: strings.TrimRight(c.Site, "/"), flavor: c.Flavor, email: c.Email, token: token,
		http: &http.Client{Timeout: 30 * time.Second}, sleep: sleepCtx, maxRetries: 4}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *client) api() string {
	if c.flavor == DataCenter {
		return c.site + "/rest/api/2"
	}
	return c.site + "/rest/api/3"
}

func (c *client) browse(key string) string { return c.site + "/browse/" + url.PathEscape(key) }

// apiError is a failed request, with a message for the user.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func statusOf(err error) int {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status
	}
	return 0
}

// do sends one request, retrying on rate limits (429) and 503 with
// Retry-After or a doubling wait, within ctx's deadline.
func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	return c.send(ctx, method, path, func() (io.Reader, string) {
		if body == nil {
			return nil, ""
		}
		return bytes.NewReader(raw), "application/json"
	}, nil, out)
}

func (c *client) send(ctx context.Context, method, path string, body func() (io.Reader, string), header http.Header, out any) error {
	wait := time.Second
	for attempt := 0; ; attempt++ {
		rd, ctype := body()
		req, err := http.NewRequestWithContext(ctx, method, c.api()+path, rd)
		if err != nil {
			return err
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range header {
			req.Header[k] = v
		}
		if c.flavor == DataCenter {
			req.Header.Set("Authorization", "Bearer "+c.token)
		} else {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token)))
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("Jira did not answer in time (%s)", c.site)
			}
			return fmt.Errorf("cannot reach Jira at %s: %v", c.site, err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < c.maxRetries {
			d := wait
			if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s >= 0 {
				d = time.Duration(s) * time.Second
			}
			if dl, ok := ctx.Deadline(); ok && time.Until(dl) < d {
				return &apiError{resp.StatusCode, "Jira is limiting requests; try again in a minute"}
			}
			if err := c.sleep(ctx, d); err != nil {
				return &apiError{resp.StatusCode, "Jira is limiting requests; try again in a minute"}
			}
			wait *= 2
			continue
		}
		if resp.StatusCode >= 300 {
			return c.failure(resp.StatusCode, data)
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("Jira answered with something that is not the expected JSON (%v)", err)
			}
		}
		return nil
	}
}

func (c *client) failure(status int, data []byte) error {
	var e struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		Message       string            `json:"message"`
	}
	_ = json.Unmarshal(data, &e)
	detail := strings.Join(e.ErrorMessages, "; ")
	for k, v := range e.Errors {
		detail = strings.TrimPrefix(detail+"; "+k+": "+v, "; ")
	}
	if detail == "" {
		detail = e.Message
	}
	switch status {
	case http.StatusUnauthorized:
		return &apiError{status, fmt.Sprintf("Jira rejected the credentials for %s; run `kalka-plugin-jira login`", c.site)}
	case http.StatusForbidden:
		return &apiError{status, "no permission in Jira" + suffix(detail)}
	case http.StatusNotFound:
		return &apiError{status, "not found or no permission"}
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return &apiError{status, "Jira is limiting requests; try again in a minute"}
	}
	return &apiError{status, fmt.Sprintf("Jira answered %d%s", status, suffix(detail))}
}

func suffix(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return ": " + s
}

// upload sends a file as a new attachment of key and returns its id.
func (c *client) upload(ctx context.Context, key, fileName string, content []byte) (string, error) {
	var out []struct {
		ID string `json:"id"`
	}
	body := func() (io.Reader, string) {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		part, _ := w.CreateFormFile("file", fileName)
		part.Write(content)
		w.Close()
		return &b, w.FormDataContentType()
	}
	h := http.Header{"X-Atlassian-Token": {"no-check"}}
	if err := c.send(ctx, http.MethodPost, "/issue/"+url.PathEscape(key)+"/attachments", body, h, &out); err != nil {
		return "", err
	}
	if len(out) == 0 || !idRe.MatchString(out[0].ID) {
		return "", errors.New("Jira did not return the attachment's id")
	}
	return out[0].ID, nil
}

func (c *client) deleteAttachment(ctx context.Context, id string) error {
	err := c.do(ctx, http.MethodDelete, "/attachment/"+url.PathEscape(id), nil, nil)
	if statusOf(err) == http.StatusNotFound {
		return nil // already gone
	}
	return err
}

// comment writes a comment on key, editing comment id when it still
// exists, and returns the comment's id.
func (c *client) comment(ctx context.Context, key, id string, body any) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	payload := map[string]any{"body": body}
	if id != "" {
		err := c.do(ctx, http.MethodPut, "/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(id), payload, &out)
		if err == nil {
			return id, nil
		}
		if statusOf(err) != http.StatusNotFound {
			return "", err
		}
		// Deleted on the issue: write a new one.
	}
	if err := c.do(ctx, http.MethodPost, "/issue/"+url.PathEscape(key)+"/comment", payload, &out); err != nil {
		return "", err
	}
	if !idRe.MatchString(out.ID) {
		return "", errors.New("Jira did not return the comment's id")
	}
	return out.ID, nil
}
