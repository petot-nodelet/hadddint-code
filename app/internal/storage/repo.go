package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sukamoto/app/db/generated"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrEmailTaken      = errors.New("email already registered")
	ErrInvalidPassword = errors.New("invalid credentials")
)

type User struct {
	ID           int64
	FullName     string
	Email        string
	Phone        string
	PasswordHash string
	Role         string
	Status       string
}

type PackageOption struct {
	ID         string
	Name       string
	Available  bool
	ComingSoon bool
}

type Template struct {
	ID          string
	Name        string
	Category    string
	Description string
	Thumbnail   string
	PackageID   string
	Packages    []PackageOption
	IsActive    bool
	ConfigJSON  []byte
	Features    []string
	SortOrder   int64
}

type PackageRow struct {
	ID           string
	Name         string
	Tagline      string
	Description  string
	PriceCents   int64
	DurationDays int64
	MaxPhotos    int64
	MaxVideos    int64
	MaxEvents    int64
	ComingSoon   bool
	Available    bool
}

type OrderSelection struct {
	ID           int64
	PackageID    string
	PackageName  string
	TemplateID   string
	TemplateName string
	Status       string
}

func (db *DB) CreateUser(ctx context.Context, fullName, email, phone, passwordHash, role string) (int64, error) {
	id, err := db.Queries.CreateUser(ctx, generated.CreateUserParams{
		FullName: fullName, Email: email, Phone: phone, PasswordHash: passwordHash, Role: role,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrEmailTaken
	}
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

func (db *DB) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row, err := db.Queries.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &User{
		ID: row.ID, FullName: row.FullName, Email: row.Email, Phone: row.Phone,
		PasswordHash: row.PasswordHash, Role: row.Role, Status: row.Status,
	}, nil
}

func (db *DB) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row, err := db.Queries.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &User{
		ID: row.ID, FullName: row.FullName, Email: row.Email, Phone: row.Phone,
		PasswordHash: row.PasswordHash, Role: row.Role, Status: row.Status,
	}, nil
}

func (db *DB) ListTemplates(ctx context.Context, packageID, category, search, sortBy string, pageSize, pageOffset int64) ([]Template, error) {
	rows, err := db.Queries.ListTemplates(ctx, generated.ListTemplatesParams{
		PackageID:  packageID,
		Category:   category,
		Search:     search,
		PageSize:   pageSize,
		PageOffset: pageOffset,
		SortBy:     sortBy,
	})
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	packages, err := db.Queries.ListTemplatePackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list template packages: %w", err)
	}
	byTemplate := make(map[string][]PackageOption, len(packages))
	for _, p := range packages {
		byTemplate[p.TemplateID] = append(byTemplate[p.TemplateID], PackageOption{
			ID: p.ID, Name: p.Name, Available: p.Available != 0, ComingSoon: p.ComingSoon != 0,
		})
	}

	out := make([]Template, 0, len(rows))
	for _, row := range rows {
		t, err := templateFromRow(row.ID, row.Name, row.Category, row.Description, row.Thumbnail,
			row.IsActive, row.Config, row.Features, row.SortOrder)
		if err != nil {
			return nil, err
		}
		t.Packages = byTemplate[t.ID]
		if len(t.Packages) > 0 {
			t.PackageID = t.Packages[0].ID
		}
		out = append(out, t)
	}
	return out, nil
}

func (db *DB) CountTemplates(ctx context.Context, packageID, category, search string) (int64, error) {
	count, err := db.Queries.CountTemplates(ctx, generated.CountTemplatesParams{
		PackageID: packageID,
		Category:  category,
		Search:    search,
	})
	if err != nil {
		return 0, fmt.Errorf("count templates: %w", err)
	}
	return count, nil
}

func (db *DB) ListTemplateCategories(ctx context.Context) ([]string, error) {
	categories, err := db.Queries.ListTemplateCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list template categories: %w", err)
	}
	return categories, nil
}

func (db *DB) GetTemplate(ctx context.Context, id string) (*Template, error) {
	row, err := db.Queries.GetTemplate(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	t, err := templateFromRow(row.ID, row.Name, row.Category, row.Description, row.Thumbnail,
		row.IsActive, row.Config, row.Features, row.SortOrder)
	if err != nil {
		return nil, err
	}
	packages, err := db.Queries.GetTemplatePackages(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get template packages: %w", err)
	}
	for _, p := range packages {
		t.Packages = append(t.Packages, PackageOption{
			ID: p.ID, Name: p.Name, Available: p.Available != 0, ComingSoon: p.ComingSoon != 0,
		})
	}
	if len(t.Packages) > 0 {
		t.PackageID = t.Packages[0].ID
	}
	return &t, nil
}

func templateFromRow(id, name, category, description, thumbnail string, isActive int64, config, features string, sortOrder int64) (Template, error) {
	var t Template
	if err := json.Unmarshal([]byte(features), &t.Features); err != nil {
		return Template{}, fmt.Errorf("decode template %q features: %w", id, err)
	}
	if err := json.Unmarshal([]byte(config), &json.RawMessage{}); err != nil {
		return Template{}, fmt.Errorf("decode template %q config: %w", id, err)
	}
	return Template{
		ID: id, Name: name, Category: category, Description: description, Thumbnail: thumbnail,
		IsActive: isActive != 0, ConfigJSON: []byte(config), Features: t.Features, SortOrder: sortOrder,
	}, nil
}

func (db *DB) ListPackages(ctx context.Context) ([]PackageRow, error) {
	rows, err := db.Queries.ListPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	out := make([]PackageRow, 0, len(rows))
	for _, p := range rows {
		out = append(out, PackageRow{
			ID: p.ID, Name: p.Name, Tagline: p.Tagline, Description: p.Description,
			PriceCents: p.PriceCents, DurationDays: p.DurationDays, MaxPhotos: p.MaxPhotos,
			MaxVideos: p.MaxVideos, MaxEvents: p.MaxEvents, ComingSoon: p.ComingSoon != 0,
			Available: p.Available != 0,
		})
	}
	return out, nil
}

func (db *DB) GetPackage(ctx context.Context, id string) (*PackageRow, error) {
	p, err := db.Queries.GetPackage(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get package: %w", err)
	}
	return &PackageRow{
		ID: p.ID, Name: p.Name, Tagline: p.Tagline, Description: p.Description,
		PriceCents: p.PriceCents, DurationDays: p.DurationDays, MaxPhotos: p.MaxPhotos,
		MaxVideos: p.MaxVideos, MaxEvents: p.MaxEvents, ComingSoon: p.ComingSoon != 0,
		Available: p.Available != 0,
	}, nil
}

func (db *DB) TemplateSupportsPackage(ctx context.Context, templateID, packageID string) (bool, error) {
	supported, err := db.Queries.TemplateSupportsPackage(ctx, generated.TemplateSupportsPackageParams{
		TemplateID: templateID, PackageID: packageID,
	})
	if err != nil {
		return false, fmt.Errorf("check template package: %w", err)
	}
	return supported, nil
}

func (db *DB) CreateOrder(ctx context.Context, userID int64, packageID, templateID string) (int64, error) {
	id, err := db.Queries.CreateOrder(ctx, generated.CreateOrderParams{
		UserID: sql.NullInt64{Int64: userID, Valid: true}, PackageID: packageID, TemplateID: templateID,
	})
	if err != nil {
		return 0, fmt.Errorf("create order: %w", err)
	}
	return id, nil
}

func (db *DB) GetOrderByIDForUser(ctx context.Context, orderID, userID int64) (*OrderSelection, error) {
	row, err := db.Queries.GetOrderByIDForUser(ctx, generated.GetOrderByIDForUserParams{
		ID: orderID, UserID: sql.NullInt64{Int64: userID, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get customer order: %w", err)
	}
	return &OrderSelection{
		ID: row.ID, PackageID: row.PackageID, PackageName: row.PackageName,
		TemplateID: row.TemplateID, TemplateName: row.TemplateName, Status: row.Status,
	}, nil
}
