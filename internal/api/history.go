package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type apiResource struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Ordinal    int    `json:"ordinal"`
	RemoteURL  string `json:"remote_url"`
	SaveStatus string `json:"save_status"`
	MIMEType   string `json:"mime_type,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	SaveError  string `json:"save_error,omitempty"`
}

type historyWorkReference struct {
	ID         int64  `json:"id"`
	PlatformID string `json:"platform_id"`
}

type historyVersionReference struct {
	ID     int64 `json:"id"`
	Number int64 `json:"number"`
}

type historyEntry struct {
	RunID        int64                    `json:"run_id"`
	RequestedURL string                   `json:"requested_url"`
	Status       string                   `json:"status"`
	Source       string                   `json:"source"`
	StartedAt    time.Time                `json:"started_at"`
	FinishedAt   *time.Time               `json:"finished_at,omitempty"`
	Work         *historyWorkReference    `json:"work,omitempty"`
	Version      *historyVersionReference `json:"version,omitempty"`
	Error        string                   `json:"error,omitempty"`
}

type historyPage struct {
	Items      []historyEntry `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type workSummary struct {
	ID          int64     `json:"id"`
	PlatformID  string    `json:"platform_id"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

type workVersionDetail struct {
	ID         int64          `json:"id"`
	Number     int64          `json:"number"`
	CapturedAt time.Time      `json:"captured_at"`
	Resources  []apiResource  `json:"resources"`
	Data       map[string]any `json:"data"`
}

type workDetail struct {
	Work     workSummary         `json:"work"`
	Versions []workVersionDetail `json:"versions"`
}

func publicResources(resources []storedResource) []apiResource {
	result := make([]apiResource, 0, len(resources))
	for _, resource := range resources {
		status := resource.SaveStatus
		if status == "stored" {
			status = "saved"
		}
		result = append(result, apiResource{
			ID:         resource.ID,
			Kind:       resource.Kind,
			Ordinal:    resource.Ordinal,
			RemoteURL:  resource.RemoteURL,
			SaveStatus: status,
			MIMEType:   resource.MIMEType,
			SizeBytes:  resource.SizeBytes,
			SHA256:     resource.SHA256,
			SaveError:  resource.SaveError,
		})
	}
	return result
}

func (s *appStore) history(ctx context.Context, limit, cursor int64) (historyPage, error) {
	if limit < 1 || limit > 100 {
		return historyPage{}, errors.New("history limit must be between 1 and 100")
	}
	query := `
		SELECT r.id, r.requested_url, r.status, r.source, r.started_at, r.finished_at,
		       r.error, w.id, w.platform_id, v.id, v.version_number
		FROM parse_runs r
		LEFT JOIN works w ON w.id = r.work_id
		LEFT JOIN work_versions v ON v.id = r.version_id
	`
	args := make([]any, 0, 2)
	if cursor > 0 {
		query += " WHERE r.id < ?"
		args = append(args, cursor)
	}
	query += " ORDER BY r.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return historyPage{}, err
	}
	defer rows.Close()

	items := make([]historyEntry, 0, limit+1)
	for rows.Next() {
		var (
			entry             historyEntry
			startedAt         int64
			finishedAt        sql.NullInt64
			errorText         sql.NullString
			workID, versionID sql.NullInt64
			platformID        sql.NullString
			versionNumber     sql.NullInt64
		)
		if err := rows.Scan(
			&entry.RunID, &entry.RequestedURL, &entry.Status, &entry.Source,
			&startedAt, &finishedAt, &errorText, &workID, &platformID,
			&versionID, &versionNumber,
		); err != nil {
			return historyPage{}, err
		}
		entry.StartedAt = time.UnixMilli(startedAt).UTC()
		if finishedAt.Valid {
			value := time.UnixMilli(finishedAt.Int64).UTC()
			entry.FinishedAt = &value
		}
		if errorText.Valid {
			entry.Error = errorText.String
		}
		if workID.Valid && platformID.Valid {
			entry.Work = &historyWorkReference{ID: workID.Int64, PlatformID: platformID.String}
		}
		if versionID.Valid && versionNumber.Valid {
			entry.Version = &historyVersionReference{ID: versionID.Int64, Number: versionNumber.Int64}
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return historyPage{}, err
	}

	page := historyPage{Items: items}
	if int64(len(items)) > limit {
		cursorValue := strconv.FormatInt(items[limit-1].RunID, 10)
		page.NextCursor = &cursorValue
		page.Items = items[:limit]
	}
	return page, nil
}

func (s *appStore) workDetail(ctx context.Context, workID int64) (workDetail, error) {
	if workID < 1 {
		return workDetail{}, sql.ErrNoRows
	}
	var (
		detail                  workDetail
		firstSeenAt, lastSeenAt int64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, platform_id, first_seen_at, last_seen_at
		FROM works WHERE id = ?
	`, workID).Scan(
		&detail.Work.ID, &detail.Work.PlatformID, &firstSeenAt, &lastSeenAt,
	); err != nil {
		return workDetail{}, err
	}
	detail.Work.FirstSeenAt = time.UnixMilli(firstSeenAt).UTC()
	detail.Work.LastSeenAt = time.UnixMilli(lastSeenAt).UTC()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, version_number, captured_at, data_json
		FROM work_versions
		WHERE work_id = ?
		ORDER BY version_number DESC
	`, workID)
	if err != nil {
		return workDetail{}, err
	}
	type pendingVersion struct {
		ID         int64
		Number     int64
		CapturedAt int64
		DataJSON   string
	}
	pending := make([]pendingVersion, 0)
	for rows.Next() {
		var version pendingVersion
		if err := rows.Scan(&version.ID, &version.Number, &version.CapturedAt, &version.DataJSON); err != nil {
			rows.Close()
			return workDetail{}, err
		}
		pending = append(pending, version)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return workDetail{}, err
	}

	detail.Versions = make([]workVersionDetail, 0, len(pending))
	for _, item := range pending {
		version := workVersionDetail{
			ID:         item.ID,
			Number:     item.Number,
			CapturedAt: time.UnixMilli(item.CapturedAt).UTC(),
		}
		if err := json.Unmarshal([]byte(item.DataJSON), &version.Data); err != nil {
			return workDetail{}, fmt.Errorf("decode work version %d: %w", item.ID, err)
		}
		resources, err := s.loadVersionResources(ctx, item.ID)
		if err != nil {
			return workDetail{}, err
		}
		version.Resources = publicResources(resources)
		detail.Versions = append(detail.Versions, version)
	}
	return detail, nil
}
