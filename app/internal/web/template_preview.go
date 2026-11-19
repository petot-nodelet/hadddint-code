package web

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/sukamoto/app/internal/storage"
)

type PreviewMedia struct {
	Cover   string   `json:"cover"`
	Gallery []string `json:"gallery"`
}

var previewColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func decodePreviewConfig(template storage.Template) (PreviewConfig, error) {
	var config PreviewConfig
	if err := json.Unmarshal(template.ConfigJSON, &config); err != nil {
		return PreviewConfig{}, fmt.Errorf("decode template %q config: %w", template.ID, err)
	}

	var err error
	if config.Palette.Background, err = configColor(config.Palette.Background, "#171416"); err != nil {
		return PreviewConfig{}, fmt.Errorf("template %q background color: %w", template.ID, err)
	}
	if config.Palette.Surface, err = configColor(config.Palette.Surface, "#f5f0e9"); err != nil {
		return PreviewConfig{}, fmt.Errorf("template %q surface color: %w", template.ID, err)
	}
	if config.Palette.Accent, err = configColor(config.Palette.Accent, "#d4af69"); err != nil {
		return PreviewConfig{}, fmt.Errorf("template %q accent color: %w", template.ID, err)
	}
	if config.Palette.Text, err = configColor(config.Palette.Text, "#fffaf2"); err != nil {
		return PreviewConfig{}, fmt.Errorf("template %q text color: %w", template.ID, err)
	}
	if config.Typography.Heading == "" {
		config.Typography.Heading = "Cormorant Garamond"
	}
	if config.Typography.Body == "" {
		config.Typography.Body = "Plus Jakarta Sans"
	}
	if !validPreviewFont(config.Typography.Heading) {
		return PreviewConfig{}, fmt.Errorf("template %q has unsupported heading font %q", template.ID, config.Typography.Heading)
	}
	if !validPreviewFont(config.Typography.Body) {
		return PreviewConfig{}, fmt.Errorf("template %q has unsupported body font %q", template.ID, config.Typography.Body)
	}
	if config.Layout == "" {
		config.Layout = "classic"
	}
	if !oneOf(config.Layout, "classic", "editorial", "story-first") {
		return PreviewConfig{}, fmt.Errorf("template %q has unsupported layout %q", template.ID, config.Layout)
	}
	if config.CoverStyle == "" {
		config.CoverStyle = "framed"
	}
	if !oneOf(config.CoverStyle, "framed", "torii", "split", "fullbleed", "parallax") {
		return PreviewConfig{}, fmt.Errorf("template %q has unsupported cover style %q", template.ID, config.CoverStyle)
	}
	if len(config.Sections) == 0 {
		config.Sections = []string{"cover", "couple", "events", "gallery", "rsvp"}
	}
	for _, section := range config.Sections {
		if !oneOf(section, "cover", "couple", "story", "events", "gallery", "rsvp", "gift") {
			return PreviewConfig{}, fmt.Errorf("template %q has unsupported section %q", template.ID, section)
		}
	}
	if config.Media.Cover != "" && (!strings.HasPrefix(config.Media.Cover, "/") || strings.HasPrefix(config.Media.Cover, "//")) {
		return PreviewConfig{}, fmt.Errorf("template %q cover image must use a local path", template.ID)
	}
	for _, image := range config.Media.Gallery {
		if !strings.HasPrefix(image, "/") || strings.HasPrefix(image, "//") {
			return PreviewConfig{}, fmt.Errorf("template %q gallery images must use local paths", template.ID)
		}
	}
	return config, nil
}

func configColor(value, fallback string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	if !previewColor.MatchString(value) {
		return "", fmt.Errorf("invalid hex value %q", value)
	}
	return value, nil
}

func validPreviewFont(font string) bool {
	return oneOf(font, "Alex Brush", "Cormorant Garamond", "Plus Jakarta Sans")
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func categoryLabel(category string) string {
	return strings.ToUpper(strings.ReplaceAll(category, "-", " "))
}

func hasAvailablePackage(packages []storage.PackageOption) bool {
	for _, pkg := range packages {
		if pkg.Available && !pkg.ComingSoon {
			return true
		}
	}
	return false
}

func sectionLabel(section string) string {
	switch section {
	case "cover":
		return "Sampul undangan"
	case "couple":
		return "Mempelai"
	case "story":
		return "Kisah kami"
	case "events":
		return "Waktu & lokasi"
	case "gallery":
		return "Galeri"
	case "rsvp":
		return "Konfirmasi kehadiran"
	case "gift":
		return "Hadiah digital"
	default:
		return section
	}
}
