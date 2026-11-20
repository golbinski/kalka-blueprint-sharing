package main

// issue_meta: one search per level of the hierarchy, in batches of up to
// 100 keys. Parents are read from the parent field, which on Jira Cloud
// covers sub-tasks, epics and any level above an epic the site has set
// up (premium plans' custom hierarchy levels); on Data Center it covers
// sub-tasks only, since epics there are linked by a custom field.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// maxParents caps the parent chain (spec 10.3).
const maxParents = 5

// batchSize is the most keys one search asks for.
const batchSize = 100

type issueStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type issueParent struct {
	Issue string `json:"issue"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
	URL   string `json:"url,omitempty"`
}

type issueMeta struct {
	Issue    string        `json:"issue"`
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Title    string        `json:"title,omitempty"`
	URL      string        `json:"url,omitempty"`
	Type     string        `json:"type,omitempty"`
	Status   *issueStatus  `json:"status,omitempty"`
	Assignee string        `json:"assignee,omitempty"`
	Labels   []string      `json:"labels,omitempty"`
	Sprint   string        `json:"sprint,omitempty"`
	Parents  []issueParent `json:"parents,omitempty"`
	Updated  string        `json:"updated,omitempty"`
}

// jiraIssue is the part of Jira's issue JSON the plugin reads. Fields is
// decoded by hand, because the sprint field's name differs per site.
type jiraIssue struct {
	Key    string                     `json:"key"`
	Fields map[string]json.RawMessage `json:"fields"`
}

type named struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Key         string `json:"key"`
}

func (j jiraIssue) str(f string) string {
	var s string
	_ = json.Unmarshal(j.Fields[f], &s)
	return s
}

func (j jiraIssue) obj(f string) named {
	var n named
	_ = json.Unmarshal(j.Fields[f], &n)
	return n
}

func (j jiraIssue) parentKey() string {
	var p struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(j.Fields["parent"], &p)
	if validKey(p.Key) {
		return p.Key
	}
	return ""
}

// category maps Jira's status category keys onto kalka's three.
func category(key string) string {
	switch key {
	case "done":
		return "done"
	case "indeterminate":
		return "in_progress"
	}
	return "todo"
}

// search reads the issues with keys, in batches. A key Jira does not know
// fails a whole JQL query, so a failed batch is read one issue at a time;
// missing issues are simply absent from the result.
func (c *client) search(ctx context.Context, keys []string, fields []string) (map[string]jiraIssue, error) {
	out := map[string]jiraIssue{}
	for start := 0; start < len(keys); start += batchSize {
		batch := keys[start:min(start+batchSize, len(keys))]
		got, err := c.searchBatch(ctx, batch, fields)
		if err != nil {
			st := statusOf(err)
			if st != http.StatusBadRequest {
				return nil, err
			}
			for _, k := range batch {
				var is jiraIssue
				err := c.do(ctx, http.MethodGet, "/issue/"+k+"?fields="+strings.Join(fields, ","), nil, &is)
				switch {
				case err == nil:
					out[k] = is
				case statusOf(err) == http.StatusNotFound:
				default:
					return nil, err
				}
			}
			continue
		}
		for _, is := range got {
			out[is.Key] = is
		}
	}
	return out, nil
}

func (c *client) searchBatch(ctx context.Context, keys []string, fields []string) ([]jiraIssue, error) {
	body := map[string]any{"jql": "key in (" + strings.Join(keys, ",") + ")", "fields": fields, "maxResults": batchSize}
	var resp struct {
		Issues []jiraIssue `json:"issues"`
	}
	path := "/search/jql" // Jira Cloud's search
	if c.flavor == DataCenter {
		path = "/search"
	}
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return resp.Issues, nil
}

// sprintField finds the site's sprint field (Jira Software), keeping the
// answer for a day in the plugin's own folder.
func (c *client) sprintField(ctx context.Context, st *store) string {
	if id, ok := st.cachedField(c.site); ok {
		return id
	}
	var fields []struct {
		ID     string `json:"id"`
		Schema struct {
			Custom string `json:"custom"`
		} `json:"schema"`
	}
	id := ""
	if err := c.do(ctx, http.MethodGet, "/field", nil, &fields); err != nil {
		return ""
	}
	for _, f := range fields {
		if f.Schema.Custom == "com.pyxis.greenhopper.jira:gh-sprint" {
			id = f.ID
			break
		}
	}
	st.saveField(c.site, id)
	return id
}

var dcSprintName = regexp.MustCompile(`name=([^,\]]*)`)

// sprintName is the active sprint if there is one, else the latest.
func sprintName(raw json.RawMessage) string {
	var cloud []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if json.Unmarshal(raw, &cloud) == nil && len(cloud) > 0 {
		for _, s := range cloud {
			if strings.EqualFold(s.State, "active") {
				return s.Name
			}
		}
		return cloud[len(cloud)-1].Name
	}
	// Data Center v2 sends each sprint as a string of name=value pairs.
	var dc []string
	if json.Unmarshal(raw, &dc) == nil && len(dc) > 0 {
		pick := dc[len(dc)-1]
		for _, s := range dc {
			if strings.Contains(s, "state=ACTIVE") {
				pick = s
			}
		}
		if m := dcSprintName.FindStringSubmatch(pick); m != nil {
			return m[1]
		}
	}
	return ""
}

// issueMeta answers issue_meta for keys.
func (c *client) issueMeta(ctx context.Context, st *store, keys []string) ([]issueMeta, error) {
	out := make([]issueMeta, len(keys))
	var valid []string
	seen := map[string]bool{}
	for i, k := range keys {
		out[i] = issueMeta{Issue: k}
		if !validKey(k) {
			out[i].Error = "not a Jira issue key (such as DEV-123)"
			continue
		}
		if !seen[k] {
			seen[k] = true
			valid = append(valid, k)
		}
	}
	if len(valid) == 0 {
		return out, nil
	}
	sprint := c.sprintField(ctx, st)
	fields := []string{"summary", "status", "issuetype", "assignee", "labels", "parent", "updated"}
	if sprint != "" {
		fields = append(fields, sprint)
	}
	issues, err := c.search(ctx, valid, fields)
	if err != nil {
		return nil, err
	}

	// Walk up: one search per level for the parents not read yet.
	known := map[string]jiraIssue{}
	for k, v := range issues {
		known[k] = v
	}
	level := map[string]bool{}
	for _, is := range issues {
		if p := is.parentKey(); p != "" {
			level[p] = true
		}
	}
	for depth := 0; depth < maxParents && len(level) > 0; depth++ {
		var ask []string
		for k := range level {
			if _, ok := known[k]; !ok {
				ask = append(ask, k)
			}
		}
		sort.Strings(ask)
		next := map[string]bool{}
		if len(ask) > 0 {
			got, err := c.search(ctx, ask, []string{"summary", "issuetype", "parent"})
			if err != nil {
				break // the issues themselves are still worth sending
			}
			for k, v := range got {
				known[k] = v
			}
		}
		for k := range level {
			if p := known[k].parentKey(); p != "" {
				next[p] = true
			}
		}
		level = next
	}

	for i, k := range keys {
		if out[i].Error != "" {
			continue
		}
		is, ok := issues[k]
		if !ok {
			out[i].Error = "not found or no permission"
			continue
		}
		m := issueMeta{Issue: k, OK: true, Title: is.str("summary"), URL: c.browse(k), Type: is.obj("issuetype").Name}
		var status struct {
			Name     string `json:"name"`
			Category struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		}
		if json.Unmarshal(is.Fields["status"], &status) == nil && status.Name != "" {
			m.Status = &issueStatus{Name: status.Name, Category: category(status.Category.Key)}
		}
		m.Assignee = is.obj("assignee").DisplayName
		_ = json.Unmarshal(is.Fields["labels"], &m.Labels)
		if sprint != "" {
			m.Sprint = sprintName(is.Fields[sprint])
		}
		if u := is.str("updated"); u != "" {
			if t, err := time.Parse("2006-01-02T15:04:05.000-0700", u); err == nil {
				m.Updated = t.UTC().Format(time.RFC3339)
			}
		}
		seen := map[string]bool{k: true}
		for p := is.parentKey(); p != "" && len(m.Parents) < maxParents && !seen[p]; {
			seen[p] = true
			pi, ok := known[p]
			par := issueParent{Issue: p, URL: c.browse(p)}
			if ok {
				par.Title, par.Type = pi.str("summary"), pi.obj("issuetype").Name
			}
			m.Parents = append(m.Parents, par)
			if !ok {
				break
			}
			p = pi.parentKey()
		}
		out[i] = m
	}
	return out, nil
}
