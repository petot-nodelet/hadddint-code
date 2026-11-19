package web

import (
	"strings"
	"testing"

	"github.com/sukamoto/app/internal/storage"
)

func TestDecodePreviewConfigValidatesRendererInputs(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "valid template config",
			json: `{"palette":{"background":"#201a18","surface":"#efeae2","accent":"#c9a27c","text":"#f8f4ee"},"typography":{"heading":"Alex Brush","body":"Plus Jakarta Sans"},"layout":"classic","cover_style":"framed","sections":["cover","gallery"],"media":{"cover":"/index/cover.jpg","gallery":["/index/gallery.jpg"]}}`,
			want: "",
		},
		{
			name: "rejects CSS injection in palette",
			json: `{"palette":{"background":"#000000; color:red"}}`,
			want: "background color",
		},
		{
			name: "rejects unsupported font",
			json: `{"typography":{"heading":"Unknown Font","body":"Plus Jakarta Sans"}}`,
			want: "unsupported heading font",
		},
		{
			name: "rejects remote cover media",
			json: `{"media":{"cover":"https://example.com/image.jpg"}}`,
			want: "local path",
		},
		{
			name: "rejects malformed configuration",
			json: `{`,
			want: "decode template",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodePreviewConfig(storage.Template{ID: "test", ConfigJSON: []byte(test.json)})
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}
