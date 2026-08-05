package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/metadata"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// hostSummary is the API's view of an inventory.Server. It deliberately
// excludes SSHKey (a filesystem path, but still connection-sensitive) and
// raw plugin config, since a plugin's config map can carry things like a
// password_env variable *name* - not a secret itself, but still not
// something to expose over the network by default.
type hostSummary struct {
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Plugins  []string `json:"plugins"`
	Schedule string   `json:"schedule"`
}

func listHostsHandler(inv *inventory.Inventory) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hosts := make([]hostSummary, 0, len(inv.Servers))
		for name, srv := range inv.Servers {
			plugins := make([]string, 0, len(srv.Plugins))
			for p := range srv.Plugins {
				plugins = append(plugins, p)
			}
			sort.Strings(plugins)
			hosts = append(hosts, hostSummary{Name: name, Host: srv.Host, Plugins: plugins, Schedule: srv.Schedule})
		}
		sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
		writeJSON(w, http.StatusOK, hosts)
	}
}

// jobSummary and snapshotSummary are the API's wire representations of
// core.Job/core.Snapshot. core's own types carry no json tags (they're
// domain types, not a wire contract), so serializing them directly would
// produce PascalCase fields inconsistent with hostSummary's camelCase -
// two conventions in the same API. These DTOs keep the wire contract
// deliberately decoupled from the domain types, same reasoning as
// hostSummary already excluding SSHKey.
type jobSummary struct {
	ID       string        `json:"id"`
	Host     string        `json:"host"`
	Plugin   string        `json:"plugin"`
	Policy   policySummary `json:"policy"`
	Status   string        `json:"status"`
	QueuedAt time.Time     `json:"queuedAt"`
}

type policySummary struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
	Yearly  int `json:"yearly"`
}

func newJobSummary(j core.Job) jobSummary {
	return jobSummary{
		ID:     j.ID,
		Host:   j.Host,
		Plugin: j.Plugin,
		Policy: policySummary{
			Daily:   j.Policy.Daily,
			Weekly:  j.Policy.Weekly,
			Monthly: j.Policy.Monthly,
			Yearly:  j.Policy.Yearly,
		},
		Status:   string(j.Status),
		QueuedAt: j.QueuedAt,
	}
}

type snapshotSummary struct {
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	Host      string    `json:"host"`
	Plugin    string    `json:"plugin"`
	Size      int64     `json:"size"`
	Checksum  string    `json:"checksum"`
	CreatedAt time.Time `json:"createdAt"`
}

func newSnapshotSummary(s core.Snapshot) snapshotSummary {
	return snapshotSummary{
		ID:        string(s.ID),
		JobID:     s.JobID,
		Host:      s.Host,
		Plugin:    s.Plugin,
		Size:      s.Size,
		Checksum:  s.Checksum,
		CreatedAt: s.CreatedAt,
	}
}

var validJobStatuses = map[core.JobStatus]bool{
	core.JobStatusQueued:     true,
	core.JobStatusInProgress: true,
	core.JobStatusCompleted:  true,
	core.JobStatusFailed:     true,
}

// listJobsHandler serves GET /v1/jobs, optionally filtered by
// ?status=queued|in_progress|completed|failed.
func listJobsHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		statusParam := r.URL.Query().Get("status")

		var jobs []core.Job
		var err error
		if statusParam != "" {
			status := core.JobStatus(statusParam)
			if !validJobStatuses[status] {
				writeError(w, http.StatusBadRequest, "status: unsupported value "+statusParam)
				return
			}
			jobs, err = md.ListJobsByStatus(ctx, status)
		} else {
			jobs, err = md.ListJobs(ctx)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		summaries := make([]jobSummary, len(jobs))
		for i, j := range jobs {
			summaries[i] = newJobSummary(j)
		}
		writeJSON(w, http.StatusOK, summaries)
	}
}

func getJobHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, err := md.GetJob(r.Context(), r.PathValue("id"))
		if errors.Is(err, metadata.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, newJobSummary(job))
	}
}

// listSnapshotsHandler serves GET /v1/snapshots?host=<name>. host is
// required, matching the existing "bop snapshot list --host" CLI command -
// metadata.Store.ListSnapshots has no all-hosts query today, and adding one
// is a separate decision from wiring up read endpoints on what already
// exists.
func listSnapshotsHandler(md *metadata.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Query().Get("host")
		if host == "" {
			writeError(w, http.StatusBadRequest, "host query parameter is required")
			return
		}
		snaps, err := md.ListSnapshots(r.Context(), host)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		summaries := make([]snapshotSummary, len(snaps))
		for i, s := range snaps {
			summaries[i] = newSnapshotSummary(s)
		}
		writeJSON(w, http.StatusOK, summaries)
	}
}
