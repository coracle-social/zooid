package zooid

import (
	"bytes"
	"log"
	"text/template"
)

type Schema struct {
	Name string
}

func (s *Schema) Render(t string) string {
	var buf bytes.Buffer
	err := template.Must(template.New("schema").Parse(t)).Execute(&buf, s)
	if err != nil {
		log.Fatal("Failed to create template: %w", err)
	}

	return buf.String()
}

func (s *Schema) Prefix(t string) string {
	return s.Render("{{.Name}}__" + t)
}
