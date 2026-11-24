package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sukamoto/app/db/generated"
)

var (
	ErrDraftConflict        = errors.New("invitation draft was changed by another request")
	ErrInvitationMediaLimit = errors.New("invitation media limit reached")
)

var storedInvitationMediaName = regexp.MustCompile(`^[a-f0-9]{36}\.(jpg|png|webp|mp3|ogg)$`)

type InvitationOrder struct {
	ID                     int64
	OrderNumber            string
	PackageID              string
	PackageName            string
	MaxPhotos              int64
	MaxVideos              int64
	MaxEvents              int64
	TemplateID             string
	TemplateName           string
	TemplateConfig         string
	DraftData              string
	DraftVersion           int64
	InvitationSlug         string
	InvitationReviewStatus string
	InvitationReviewNote   string
	PaymentStatus          string
	WorkStatus             string
}

type InvitationFeatures struct {
	RSVPEnabled        bool
	GuestbookEnabled   bool
	DigitalEnvelope    bool
	BackgroundMusic    bool
	GuestGreeting      bool
	CoupleStory        bool
	CustomColors       bool
	CustomFonts        bool
	CustomLayout       bool
	CustomCover        bool
	CustomBackground   bool
	CustomSections     bool
	CustomWording      bool
	CustomMusic        bool
	CustomURL          bool
	MultipleEvents     bool
	EventCustomization bool
}

type InvitationMedia struct {
	ID         int64
	OrderID    int64
	MediaType  string
	StoredName string
	MimeType   string
	SizeBytes  int64
	Caption    string
	SortOrder  int64
	CreatedAt  string
}

type MusicTrack = generated.MusicTrack

func (db *DB) GetInvitationOrder(ctx context.Context, orderNumber string) (InvitationOrder, error) {
	row, err := db.Queries.GetInvitationDraftByOrderNumber(ctx, sql.NullString{String: orderNumber, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationOrder{}, ErrNotFound
	}
	if err != nil {
		return InvitationOrder{}, fmt.Errorf("load invitation order: %w", err)
	}
	return InvitationOrder{
		ID: row.ID, OrderNumber: row.OrderNumber.String, PackageID: row.PackageID,
		PackageName: row.PackageName, MaxPhotos: row.MaxPhotos, MaxVideos: row.MaxVideos,
		MaxEvents: row.MaxEvents, TemplateID: row.TemplateID, TemplateName: row.TemplateName,
		TemplateConfig: row.TemplateConfig, DraftData: row.DraftData, DraftVersion: row.DraftVersion,
		InvitationSlug: row.InvitationSlug.String, InvitationReviewStatus: row.InvitationReviewStatus,
		InvitationReviewNote: row.InvitationReviewNote, PaymentStatus: row.PaymentStatus, WorkStatus: row.WorkStatus,
	}, nil
}

func (db *DB) GetAdminInvitationOrder(ctx context.Context, orderNumber string) (InvitationOrder, error) {
	row, err := db.Queries.GetAdminInvitationDraft(ctx, sql.NullString{String: orderNumber, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationOrder{}, ErrNotFound
	}
	if err != nil {
		return InvitationOrder{}, fmt.Errorf("load admin invitation draft: %w", err)
	}
	return InvitationOrder{
		ID: row.ID, OrderNumber: row.OrderNumber.String, PackageID: row.PackageID,
		PackageName: row.PackageName, MaxPhotos: row.MaxPhotos, MaxVideos: row.MaxVideos,
		MaxEvents: row.MaxEvents, TemplateID: row.TemplateID, TemplateName: row.TemplateName,
		TemplateConfig: row.TemplateConfig, DraftData: row.DraftData, DraftVersion: row.DraftVersion,
		InvitationSlug: row.InvitationSlug.String, InvitationReviewStatus: row.InvitationReviewStatus,
		InvitationReviewNote: row.InvitationReviewNote, PaymentStatus: row.PaymentStatus, WorkStatus: row.WorkStatus,
	}, nil
}

func (db *DB) GetInvitationFeatures(ctx context.Context, packageID string) (InvitationFeatures, error) {
	row, err := db.Queries.GetInvitationPackageFeatures(ctx, packageID)
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationFeatures{}, ErrNotFound
	}
	if err != nil {
		return InvitationFeatures{}, fmt.Errorf("load invitation package features: %w", err)
	}
	return InvitationFeatures{
		RSVPEnabled: row.RsvpEnabled == 1, GuestbookEnabled: row.GuestbookEnabled == 1,
		DigitalEnvelope: row.DigitalEnvelope == 1, BackgroundMusic: row.BackgroundMusic == 1,
		GuestGreeting: row.GuestGreeting == 1, CoupleStory: row.CoupleStory == 1,
		CustomColors: row.CustomColors == 1, CustomFonts: row.CustomFonts == 1,
		CustomLayout: row.CustomLayout == 1, CustomCover: row.CustomCover == 1,
		CustomBackground: row.CustomBackground == 1, CustomSections: row.CustomSections == 1,
		CustomWording: row.CustomWording == 1, CustomMusic: row.CustomMusic == 1,
		CustomURL: row.CustomUrl == 1, MultipleEvents: row.MultipleEvents == 1,
		EventCustomization: row.EventCustomization == 1,
	}, nil
}

func (db *DB) SaveInvitationDraft(ctx context.Context, orderID, expectedVersion int64, draftJSON, slug string) error {
	rows, err := db.Queries.SaveInvitationDraft(ctx, generated.SaveInvitationDraftParams{
		DraftData: draftJSON, InvitationSlug: sql.NullString{String: strings.TrimSpace(slug), Valid: strings.TrimSpace(slug) != ""},
		ID: orderID, DraftVersion: expectedVersion,
	})
	if err != nil {
		return fmt.Errorf("save invitation draft: %w", err)
	}
	if rows != 1 {
		return ErrDraftConflict
	}
	return nil
}

func (db *DB) SaveInvitationMediaDetails(ctx context.Context, orderID, mediaID, expectedVersion int64, draftJSON, slug, caption string, sortOrder int64) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invitation media details transaction: %w", err)
	}
	defer tx.Rollback()
	q := db.Queries.WithTx(tx)
	rows, err := q.SaveInvitationDraft(ctx, generated.SaveInvitationDraftParams{
		DraftData: draftJSON, InvitationSlug: sql.NullString{String: strings.TrimSpace(slug), Valid: strings.TrimSpace(slug) != ""},
		ID: orderID, DraftVersion: expectedVersion,
	})
	if err != nil {
		return fmt.Errorf("save draft with invitation media details: %w", err)
	}
	if rows != 1 {
		return ErrDraftConflict
	}
	rows, err = q.UpdateInvitationMediaCaption(ctx, generated.UpdateInvitationMediaCaptionParams{
		Caption: caption, SortOrder: sortOrder, ID: mediaID, OrderID: orderID,
	})
	if err != nil {
		return fmt.Errorf("update invitation media details: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit invitation media details: %w", err)
	}
	return nil
}

func (db *DB) ListInvitationMedia(ctx context.Context, orderID int64) ([]InvitationMedia, error) {
	rows, err := db.Queries.ListInvitationMedia(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list invitation media: %w", err)
	}
	media := make([]InvitationMedia, 0, len(rows))
	for _, row := range rows {
		media = append(media, invitationMedia(row))
	}
	return media, nil
}

func (db *DB) GetInvitationMedia(ctx context.Context, orderID, mediaID int64) (InvitationMedia, error) {
	row, err := db.Queries.GetInvitationMedia(ctx, generated.GetInvitationMediaParams{ID: mediaID, OrderID: orderID})
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationMedia{}, ErrNotFound
	}
	if err != nil {
		return InvitationMedia{}, fmt.Errorf("load invitation media: %w", err)
	}
	return invitationMedia(row), nil
}

func invitationMedia(row generated.InvitationMedium) InvitationMedia {
	return InvitationMedia{
		ID: row.ID, OrderID: row.OrderID, MediaType: row.MediaType,
		StoredName: row.StoredName, MimeType: row.MimeType, SizeBytes: row.SizeBytes,
		Caption: row.Caption, SortOrder: row.SortOrder, CreatedAt: row.CreatedAt,
	}
}

func (db *DB) CreateInvitationMedia(ctx context.Context, media InvitationMedia, maxPhotos int64, allowCustomMusic bool) (int64, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin invitation media transaction: %w", err)
	}
	defer tx.Rollback()
	q := db.Queries.WithTx(tx)
	if media.MediaType == "audio" {
		if !allowCustomMusic {
			return 0, ErrInvitationMediaLimit
		}
		count, err := q.CountInvitationAudio(ctx, media.OrderID)
		if err != nil {
			return 0, fmt.Errorf("count invitation audio: %w", err)
		}
		if count >= 1 {
			return 0, ErrInvitationMediaLimit
		}
	} else {
		count, err := q.CountInvitationMediaByType(ctx, media.OrderID)
		if err != nil {
			return 0, fmt.Errorf("count invitation images: %w", err)
		}
		if maxPhotos <= 0 || count >= maxPhotos {
			return 0, ErrInvitationMediaLimit
		}
	}
	id, err := q.InsertInvitationMedia(ctx, generated.InsertInvitationMediaParams{
		OrderID: media.OrderID, MediaType: media.MediaType, StoredName: media.StoredName,
		MimeType: media.MimeType, SizeBytes: media.SizeBytes, Caption: media.Caption, SortOrder: media.SortOrder,
	})
	if err != nil {
		return 0, fmt.Errorf("insert invitation media: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit invitation media: %w", err)
	}
	return id, nil
}

func (db *DB) DeleteInvitationMedia(ctx context.Context, orderID, mediaID int64) error {
	rows, err := db.Queries.DeleteInvitationMedia(ctx, generated.DeleteInvitationMediaParams{ID: mediaID, OrderID: orderID})
	if err != nil {
		return fmt.Errorf("delete invitation media: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteInvitationMediaWithDraft(ctx context.Context, orderID, mediaID, expectedVersion int64, draftJSON, slug string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invitation media deletion: %w", err)
	}
	defer tx.Rollback()
	q := db.Queries.WithTx(tx)
	rows, err := q.SaveInvitationDraft(ctx, generated.SaveInvitationDraftParams{
		DraftData: draftJSON, InvitationSlug: sql.NullString{String: strings.TrimSpace(slug), Valid: strings.TrimSpace(slug) != ""},
		ID: orderID, DraftVersion: expectedVersion,
	})
	if err != nil {
		return fmt.Errorf("unlink invitation media from draft: %w", err)
	}
	if rows != 1 {
		return ErrDraftConflict
	}
	rows, err = q.DeleteInvitationMedia(ctx, generated.DeleteInvitationMediaParams{ID: mediaID, OrderID: orderID})
	if err != nil {
		return fmt.Errorf("delete invitation media metadata: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit invitation media deletion: %w", err)
	}
	return nil
}

func (db *DB) SaveInvitationMediaCaption(ctx context.Context, orderID, mediaID int64, caption string, sortOrder int64) error {
	rows, err := db.Queries.UpdateInvitationMediaCaption(ctx, generated.UpdateInvitationMediaCaptionParams{
		Caption: caption, SortOrder: sortOrder, ID: mediaID, OrderID: orderID,
	})
	if err != nil {
		return fmt.Errorf("save invitation media caption: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) CleanupOrphanedInvitationMedia(ctx context.Context, olderThan time.Duration) error {
	if db.UploadDir == "" {
		return nil
	}
	directory := filepath.Join(db.UploadDir, "invitation-media")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read invitation media directory: %w", err)
	}
	rows, err := db.SQL.QueryContext(ctx, "SELECT stored_name FROM invitation_media")
	if err != nil {
		return fmt.Errorf("list stored invitation media: %w", err)
	}
	referenced := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scan stored invitation media: %w", err)
		}
		referenced[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate stored invitation media: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close stored invitation media query: %w", err)
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		name := entry.Name()
		if !storedInvitationMediaName.MatchString(name) || !entry.Type().IsRegular() {
			continue
		}
		if _, ok := referenced[name]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect orphaned invitation media %q: %w", name, err)
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove orphaned invitation media %q: %w", name, err)
		}
	}
	return nil
}

func (db *DB) InvitationSlugAvailable(ctx context.Context, slug string, orderID int64) (bool, error) {
	available, err := db.Queries.IsInvitationSlugAvailable(ctx, generated.IsInvitationSlugAvailableParams{
		InvitationSlug: sql.NullString{String: slug, Valid: slug != ""}, ID: orderID,
	})
	if err != nil {
		return false, fmt.Errorf("check invitation slug: %w", err)
	}
	return available, nil
}

func (db *DB) ListMusicTracks(ctx context.Context, activeOnly bool) ([]generated.MusicTrack, error) {
	if activeOnly {
		tracks, err := db.Queries.ListActiveMusicTracks(ctx)
		if err != nil {
			return nil, fmt.Errorf("list active music tracks: %w", err)
		}
		return tracks, nil
	}
	tracks, err := db.Queries.ListMusicTracks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list music tracks: %w", err)
	}
	return tracks, nil
}

func (db *DB) GetMusicTrack(ctx context.Context, id int64) (generated.MusicTrack, error) {
	track, err := db.Queries.GetMusicTrack(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return generated.MusicTrack{}, ErrNotFound
	}
	if err != nil {
		return generated.MusicTrack{}, fmt.Errorf("load music track: %w", err)
	}
	return track, nil
}

func (db *DB) InsertMusicTrack(ctx context.Context, title, storedName, mimeType string, size int64) (int64, error) {
	id, err := db.Queries.InsertMusicTrack(ctx, generated.InsertMusicTrackParams{
		Title: title, StoredName: storedName, MimeType: mimeType, SizeBytes: size,
	})
	if err != nil {
		return 0, fmt.Errorf("save music track: %w", err)
	}
	return id, nil
}

func (db *DB) SetMusicTrackActive(ctx context.Context, id int64, active bool) error {
	value := int64(0)
	if active {
		value = 1
	}
	rows, err := db.Queries.SetMusicTrackActive(ctx, generated.SetMusicTrackActiveParams{IsActive: value, ID: id})
	if err != nil {
		return fmt.Errorf("update music track state: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) UpdateInvitationReview(ctx context.Context, orderNumber, status, note string) error {
	rows, err := db.Queries.UpdateInvitationReview(ctx, generated.UpdateInvitationReviewParams{
		InvitationReviewStatus: status, InvitationReviewNote: note,
		OrderNumber: sql.NullString{String: orderNumber, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("update invitation review: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
