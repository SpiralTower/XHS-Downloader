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

type workListCursor struct {
	LastParsedAt int64 `json:"last_parsed_at"`
	ID           int64 `json:"id"`
}

type workListItem struct {
	ID                  int64      `json:"id"`
	PlatformID          string     `json:"platform_id"`
	ParseCount          int64      `json:"parse_count"`
	VersionCount        int64      `json:"version_count"`
	LastParsedAt        *time.Time `json:"last_parsed_at,omitempty"`
	Title               string     `json:"title,omitempty"`
	ThumbnailResourceID *int64     `json:"thumbnail_resource_id,omitempty"`
	sortLastParsedAt    int64
}

type workPage struct {
	Items      []workListItem
	NextCursor *workListCursor
}

type popularWork struct {
	ID         int64  `json:"id"`
	PlatformID string `json:"platform_id"`
	Title      string `json:"title,omitempty"`
	ParseCount int64  `json:"parse_count"`
}

type workSummary struct {
	ID           int64      `json:"id"`
	PlatformID   string     `json:"platform_id"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	ParseCount   int64      `json:"parse_count"`
	LastParsedAt *time.Time `json:"last_parsed_at,omitempty"`
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

func (s *appStore) listWorks(
	ctx context.Context,
	limit int64,
	cursor workListCursor,
) (workPage, error) {
	if limit < 1 || limit > 100 {
		return workPage{}, errors.New("works limit must be between 1 and 100")
	}
	if cursor.ID < 0 || cursor.LastParsedAt < 0 || (cursor.ID == 0 && cursor.LastParsedAt != 0) {
		return workPage{}, errors.New("works cursor is invalid")
	}
	query := `
		SELECT w.id, w.platform_id, w.parse_count,
		       (SELECT COUNT(*) FROM work_versions versions WHERE versions.work_id = w.id),
		       COALESCE(w.last_parsed_at, 0),
		       COALESCE((
		           SELECT CAST(json_extract(v.data_json, '$."作品标题"') AS TEXT)
		           FROM work_versions v
		           JOIN version_resources text_resource ON text_resource.version_id = v.id
		           WHERE v.work_id = w.id
		             AND text_resource.kind = 'text'
		             AND text_resource.save_status = 'stored'
		           ORDER BY v.version_number DESC, text_resource.id DESC
		           LIMIT 1
		       ), ''),
		       (
		           SELECT image_resource.id
		           FROM work_versions v
		           JOIN version_resources image_resource ON image_resource.version_id = v.id
		           WHERE v.work_id = w.id
		             AND image_resource.kind = 'image'
		             AND image_resource.save_status = 'stored'
		             AND image_resource.relative_path IS NOT NULL
		             AND image_resource.relative_path <> ''
		           ORDER BY v.version_number DESC, image_resource.ordinal, image_resource.id
		           LIMIT 1
		       )
		FROM works w
	`
	args := make([]any, 0, 4)
	if cursor.ID > 0 && cursor.LastParsedAt > 0 {
		query += `
			WHERE w.last_parsed_at < ?
			   OR (w.last_parsed_at = ? AND w.id < ?)
			   OR w.last_parsed_at IS NULL
		`
		args = append(args, cursor.LastParsedAt, cursor.LastParsedAt, cursor.ID)
	} else if cursor.ID > 0 {
		query += ` WHERE w.last_parsed_at IS NULL AND w.id < ?`
		args = append(args, cursor.ID)
	}
	query += ` ORDER BY w.last_parsed_at DESC, w.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return workPage{}, err
	}
	defer rows.Close()
	items := make([]workListItem, 0, limit+1)
	for rows.Next() {
		var item workListItem
		var thumbnailID sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.PlatformID, &item.ParseCount, &item.VersionCount,
			&item.sortLastParsedAt, &item.Title, &thumbnailID,
		); err != nil {
			return workPage{}, err
		}
		if item.sortLastParsedAt > 0 {
			value := time.UnixMilli(item.sortLastParsedAt).UTC()
			item.LastParsedAt = &value
		}
		if thumbnailID.Valid {
			value := thumbnailID.Int64
			item.ThumbnailResourceID = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workPage{}, err
	}
	page := workPage{Items: items}
	if int64(len(items)) > limit {
		last := items[limit-1]
		page.NextCursor = &workListCursor{LastParsedAt: last.sortLastParsedAt, ID: last.ID}
		page.Items = items[:limit]
	}
	return page, nil
}

func (s *appStore) popularWorks(ctx context.Context, days, limit int) ([]popularWork, error) {
	return s.popularWorksAt(ctx, days, limit, time.Now())
}

func (s *appStore) popularWorksAt(
	ctx context.Context,
	days, limit int,
	now time.Time,
) ([]popularWork, error) {
	if days != 0 && days != 7 && days != 30 {
		return nil, errors.New("popular works days must be 0, 7, or 30")
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("popular works limit must be between 1 and 100")
	}
	const titleQuery = `
		COALESCE((
			SELECT CAST(json_extract(v.data_json, '$."作品标题"') AS TEXT)
			FROM work_versions v
			JOIN version_resources text_resource ON text_resource.version_id = v.id
			WHERE v.work_id = w.id
			  AND text_resource.kind = 'text'
			  AND text_resource.save_status = 'stored'
			ORDER BY v.version_number DESC, text_resource.id DESC
			LIMIT 1
		), '')
	`
	var query string
	var args []any
	if days == 0 {
		query = `
			SELECT w.id, w.platform_id, ` + titleQuery + `, w.parse_count
			FROM works w
			WHERE w.parse_count > 0
			ORDER BY w.parse_count DESC, w.last_parsed_at DESC, w.id DESC
			LIMIT ?
		`
		args = []any{limit}
	} else {
		now = now.UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		start := today.AddDate(0, 0, -(days - 1))
		end := today.AddDate(0, 0, 1)
		query = `
			SELECT w.id, w.platform_id, ` + titleQuery + `, SUM(daily.parse_count)
			FROM work_parse_daily daily
			JOIN works w ON w.id = daily.work_id
			WHERE daily.day_start >= ? AND daily.day_start < ?
			GROUP BY w.id
			HAVING SUM(daily.parse_count) > 0
			ORDER BY SUM(daily.parse_count) DESC,
			         w.last_parsed_at DESC, w.id DESC
			LIMIT ?
		`
		args = []any{unixMillis(start), unixMillis(end), limit}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]popularWork, 0, limit)
	for rows.Next() {
		var item popularWork
		if err := rows.Scan(&item.ID, &item.PlatformID, &item.Title, &item.ParseCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *appStore) workDetail(ctx context.Context, workID int64) (workDetail, error) {
	if workID < 1 {
		return workDetail{}, sql.ErrNoRows
	}
	var (
		detail                  workDetail
		firstSeenAt, lastSeenAt int64
		lastParsedAt            sql.NullInt64
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, platform_id, first_seen_at, last_seen_at, parse_count, last_parsed_at
		FROM works WHERE id = ?
	`, workID).Scan(
		&detail.Work.ID, &detail.Work.PlatformID, &firstSeenAt, &lastSeenAt,
		&detail.Work.ParseCount, &lastParsedAt,
	); err != nil {
		return workDetail{}, err
	}
	detail.Work.FirstSeenAt = time.UnixMilli(firstSeenAt).UTC()
	detail.Work.LastSeenAt = time.UnixMilli(lastSeenAt).UTC()
	if lastParsedAt.Valid {
		value := time.UnixMilli(lastParsedAt.Int64).UTC()
		detail.Work.LastParsedAt = &value
	}

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
